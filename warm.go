// warm.go — Warm: bounded-concurrency cache preloading (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares WarmResult and implements Warm[T] — preload a key set
// into the cache before traffic arrives (startup, post-deploy) so the first
// users don't all stampede a cold cache. The WHY: avoid a thundering herd on
// a fresh process. Invariant: loads run with bounded concurrency
// (concurrency < 1 => 1); a per-key loader failure is REPORTED in
// WarmResult, not fatal, so one bad key cannot abort the whole warmup;
// values are stored with SetT semantics (plain codec) — read back with
// GetT/Remember.
//
// AI-context: ctx cancellation is checked with PRIORITY before the
// slot-acquire select (a bare two-case select picks randomly, so an
// already-cancelled ctx could leak the first key through) — every remaining
// key including the first is then marked with ctx.Err().

package cache

import (
	"context"
	"sync"
	"time"
)

// WarmResult reports the outcome of a single key's warmup.
type WarmResult struct {
	Key string
	Err error // nil on success; ErrNotFound if the loader had no value
}

// Warm preloads keys into c before traffic arrives (e.g. at startup, after a
// deploy) so the first users don't all stampede a cold cache. Loads run with
// bounded concurrency; a per-key loader failure is reported, not fatal, so one
// bad key can't abort the warmup. Honors ctx cancellation.
//
//	res := cache.Warm(ctx, c, hotIDs, 5*time.Minute, 16,
//	    func(ctx context.Context, id string) (Product, error) {
//	        return db.Product(ctx, id)
//	    })
//
// concurrency < 1 is treated as 1. Values are stored with SetT semantics
// (plain codec); read them back with GetT/Remember.
func Warm[T any](ctx context.Context, c Cache, keys []string, ttl time.Duration,
	concurrency int, load func(ctx context.Context, key string) (T, error),
	opts ...RememberOpt,
) []WarmResult {
	if concurrency < 1 {
		concurrency = 1
	}
	results := make([]WarmResult, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, k := range keys {
		// Check cancellation with priority before the slot-acquire select
		// below. A bare two-case select (ctx.Done vs sem-send) picks a ready
		// case at random, so an already-cancelled context could still let the
		// first key through before cancellation is observed. This explicit
		// pre-check guarantees a cancelled warmup marks every remaining key —
		// including the first — with ctx.Err() instead of silently loading it.
		if err := ctx.Err(); err != nil {
			for j := i; j < len(keys); j++ {
				results[j] = WarmResult{Key: keys[j], Err: err}
			}
			wg.Wait()
			return results
		}
		select {
		case <-ctx.Done():
			// Mark remaining keys as cancelled and stop scheduling. Reached
			// when the context is cancelled mid-warmup while we are blocked
			// waiting for a concurrency slot to free up.
			for j := i; j < len(keys); j++ {
				results[j] = WarmResult{Key: keys[j], Err: ctx.Err()}
			}
			wg.Wait()
			return results
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			v, err := load(ctx, key)
			if err != nil {
				results[idx] = WarmResult{Key: key, Err: err}
				return
			}
			results[idx] = WarmResult{Key: key, Err: SetT(ctx, c, key, v, ttl, opts...)}
		}(i, k)
	}
	wg.Wait()
	return results
}
