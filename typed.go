// typed.go — Typed[T]: a generics wrapper so a whole module works with T (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements Typed[T] and NewTyped — a thin view that carries a
// default codec + RememberOpts applied to every Get/Set/Remember/Del so
// callers stop passing options at every call site. The WHY: ergonomics over
// the bytes Cache for a single value type. Invariant: Raw() exposes the same
// underlying keyspace, so a key written via Typed.Set is readable via
// Raw().Get as a codec payload (decode accordingly) — they are not separate
// stores.
//
// AI-context: pure delegation — every method forwards to the package-level
// GetT/SetT/Remember with t.opts appended; no independent caching logic
// lives here, so behavior changes belong in remember.go, not this file.

package cache

import (
	"context"
	"time"
)

// Typed is a generics wrapper so a whole module works with T instead of
// []byte. It carries a default codec and Remember options applied to every
// call.
//
//	users := cache.NewTyped[User](backend, cache.WithJitter(0.1))
//	u, err := users.Remember(ctx, "user:42", time.Minute, loadUser)
type Typed[T any] struct {
	c    Cache
	opts []RememberOpt
}

// NewTyped wraps c. opts are applied to every Get/Set/Remember on this view.
func NewTyped[T any](c Cache, opts ...RememberOpt) *Typed[T] {
	return &Typed[T]{c: c, opts: opts}
}

// Raw returns the underlying bytes-level cache. Use it for ops the typed view
// does not expose (Has, TTL, Incr, iteration) or to share one backend between
// a Typed[T] view and raw byte access — they operate on the same keyspace, so
// a key written via Set here is readable via Raw().Get (as an envelope/codec
// payload, decode accordingly).
func (t *Typed[T]) Raw() Cache { return t.c }

// Get reads and decodes the typed value at key.
func (t *Typed[T]) Get(ctx context.Context, key string) (T, error) {
	return GetT[T](ctx, t.c, key, t.opts...)
}

// Set encodes and stores v at key.
func (t *Typed[T]) Set(ctx context.Context, key string, v T, ttl time.Duration) error {
	return SetT[T](ctx, t.c, key, v, ttl, t.opts...)
}

// Remember returns the cached value or single-flights fn on miss.
func (t *Typed[T]) Remember(ctx context.Context, key string, ttl time.Duration, fn LoadFn[T]) (T, error) {
	return Remember[T](ctx, t.c, key, ttl, fn, t.opts...)
}

// Del removes keys.
func (t *Typed[T]) Del(ctx context.Context, keys ...string) error {
	return t.c.Del(ctx, keys...)
}
