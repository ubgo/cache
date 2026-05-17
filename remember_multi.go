// remember_multi.go — batch load-through that collapses the cache N+1 (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements RememberMulti and the MultiLoadFn type — serve all
// cached keys from one GetMulti, load every miss in one fn call, write back
// with one SetMulti, turning the per-key Remember loop into two round-trips
// total. The WHY: kills the classic cache N+1. Invariant: values are stored
// with the PLAIN codec (no SWR/refresh-ahead envelope) so reads must use
// GetT/RememberMulti, not Remember; duplicate input keys are de-duped so a
// repeated id is neither loaded nor listed twice; staleness opts are ignored
// here by design (only WithJitter/WithCodec apply).
//
// AI-context: missing keys the loader omits are simply treated as not-found
// (not cached negatively here). Undecodable cached bytes => treated as a
// miss and reloaded rather than surfacing a decode error.

package cache

import (
	"context"
	"time"
)

// MultiLoadFn loads every still-missing key in one call (e.g. one
// `WHERE id IN (...)` query). It returns only the keys it could resolve;
// absent keys are treated as not-found and simply omitted from the result.
type MultiLoadFn[T any] func(ctx context.Context, missing []string) (map[string]T, error)

// RememberMulti is the batch form of Remember: it serves cached keys from a
// single GetMulti and loads every miss in one fn call, then writes them back
// with one SetMulti. This collapses the classic N+1 (loop calling Remember per
// key) into two round-trips total.
//
//	users, err := cache.RememberMulti(ctx, c, ids, time.Minute,
//	    func(ctx context.Context, miss []string) (map[string]User, error) {
//	        return db.UsersByIDs(ctx, miss) // one query for all misses
//	    })
//
// Values are stored with the plain codec (no SWR/refresh-ahead envelope);
// pair reads with GetT or another RememberMulti. WithJitter and WithCodec
// apply; staleness options are ignored here by design.
func RememberMulti[T any](ctx context.Context, c Cache, keys []string, ttl time.Duration,
	fn MultiLoadFn[T], opts ...RememberOpt,
) (map[string]T, error) {
	cfg := newRememberCfg(opts)
	out := make(map[string]T, len(keys))
	if len(keys) == 0 {
		return out, nil
	}

	raw, err := c.GetMulti(ctx, keys)
	if err != nil {
		return nil, err
	}

	// Partition requested keys into hits (decoded into out) and misses.
	// seen dedups the input so a caller passing the same id twice does not
	// load it twice or list it twice in the missing slice.
	missing := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if b, ok := raw[k]; ok {
			var v T
			if derr := cfg.codec.Decode(b, &v); derr == nil {
				out[k] = v
				continue
			}
			// Undecodable cached bytes: treat as a miss and reload.
		}
		missing = append(missing, k)
	}

	if len(missing) == 0 {
		return out, nil
	}

	loaded, err := fn(ctx, missing)
	if err != nil {
		return nil, err
	}

	items := make(map[string]Item, len(loaded))
	for k, v := range loaded {
		b, encErr := cfg.codec.Encode(v)
		if encErr != nil {
			return nil, encErr
		}
		items[k] = Item{Value: b, TTL: cfg.applyJitter(ttl)}
		out[k] = v
	}
	if len(items) > 0 {
		if err := c.SetMulti(ctx, items); err != nil {
			return nil, err
		}
	}
	return out, nil
}
