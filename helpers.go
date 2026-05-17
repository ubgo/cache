// helpers.go — Memoize, QueryKey, and Once: caller-facing convenience helpers (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements Memoize (cache-backed single-flight memoization of
// a function), QueryKey (deterministic collision-resistant key from a name +
// arbitrary parts), and Once (run a side-effecting fn at most once per
// idempotency key, cache its typed result). The WHY: common call-site
// patterns built on Remember/SetNX. Key invariant: QueryKey length-prefixes
// each part before hashing so ("a","bc") and ("ab","c") never collide; Once
// deliberately does NOT cache failures (a transient error must stay
// retryable — "succeed exactly once", not "attempt exactly once").
//
// AI-context: Once's concurrency model — first caller takes a SetNX lease
// and runs fn; a concurrent caller before the result is stored gets
// ErrInFlight; a later caller within ttl gets the cached result without
// running fn. Memoize composes straight onto Remember (remember.go).

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Memoize wraps a pure-ish function with cache-backed single-flight memoation.
// The returned function caches each distinct argument's result for ttl.
//
//	getUser := cache.Memoize(c, "user", time.Minute,
//	    func(ctx context.Context, id int) (User, error) { return db.User(ctx, id) })
//	u, err := getUser(ctx, 42) // DB hit once; subsequent calls served from cache
//
// keyPart must render the argument to a stable string; it is namespaced under
// prefix. Concurrent calls for the same argument collapse to one load.
func Memoize[K any, V any](c Cache, prefix string, ttl time.Duration,
	fn func(ctx context.Context, arg K) (V, error),
	keyPart func(K) string, opts ...RememberOpt,
) func(ctx context.Context, arg K) (V, error) {
	return func(ctx context.Context, arg K) (V, error) {
		key := prefix + ":" + keyPart(arg)
		return Remember(ctx, c, key, ttl, func(ctx context.Context) (V, error) {
			return fn(ctx, arg)
		}, opts...)
	}
}

// QueryKey builds a deterministic, collision-resistant cache key from a
// logical name plus arbitrary parts (query string + bound args, filter values,
// etc.). Parts are length-prefixed before hashing so ("a","bc") and ("ab","c")
// never collide.
//
//	key := cache.QueryKey("users.byOrg", orgID, page, sort)
func QueryKey(name string, parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		s := fmt.Sprintf("%v", p)
		_, _ = fmt.Fprintf(h, "%d:%s|", len(s), s)
	}
	return name + ":" + hex.EncodeToString(h.Sum(nil)[:16])
}

// ErrInFlight is returned by Once when another caller currently holds the
// idempotency lease and no result is cached yet. Callers may retry shortly.
var ErrInFlight = errors.New("cache: idempotent operation already in flight")

// Once runs fn at most once per idempotency key and caches its (typed) result
// for ttl, so a retried request (duplicate webhook, double-clicked payment)
// returns the original result instead of executing again.
//
//   - First caller: acquires a SetNX lease, runs fn, stores the result.
//   - Concurrent caller before the result is stored: gets ErrInFlight.
//   - Later caller within ttl: gets the cached result without running fn.
//
// fn must be the side-effecting operation whose result you want deduplicated.
func Once[T any](ctx context.Context, c Cache, key string, ttl time.Duration,
	fn func(ctx context.Context) (T, error), opts ...RememberOpt,
) (T, error) {
	cfg := newRememberCfg(opts)
	var zero T

	resultKey := "idemp:res:" + key
	if raw, err := c.Get(ctx, resultKey); err == nil {
		var v T
		if derr := cfg.codec.Decode(raw, &v); derr == nil {
			return v, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return zero, err
	}

	leaseKey := "idemp:lock:" + key
	got, err := c.SetNX(ctx, leaseKey, []byte{1}, ttl)
	if err != nil {
		return zero, err
	}
	if !got {
		// Someone else owns the lease. If they already stored the result,
		// return it; otherwise signal in-flight.
		if raw, gerr := c.Get(ctx, resultKey); gerr == nil {
			var v T
			if derr := cfg.codec.Decode(raw, &v); derr == nil {
				return v, nil
			}
		}
		return zero, ErrInFlight
	}

	// We hold the lease and no result is cached: run the side-effecting fn.
	v, err := fn(ctx)
	if err != nil {
		// Release the lease so a later retry can re-attempt. We deliberately
		// do NOT cache the failure: Once is for "succeed exactly once", so a
		// transient failure must remain retryable. A concurrent caller in the
		// gap before this Del sees the lease and gets ErrInFlight (retry).
		_ = c.Del(ctx, leaseKey)
		return zero, err
	}
	// Success: cache the result for ttl. The lease is left to expire on its
	// own ttl; the cached result short-circuits future callers before they
	// ever consult the lease, so deleting it here would be pointless churn.
	// Encode failure is non-fatal — the caller still gets the real value,
	// only deduplication of a future retry is lost.
	if b, encErr := cfg.codec.Encode(v); encErr == nil {
		_ = c.Set(ctx, resultKey, b, ttl)
	}
	return v, nil
}
