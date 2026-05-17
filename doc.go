// Package cache is a bytes-level cache contract plus the ergonomics layered on
// top of it: typed generics helpers, a single-flight Remember with
// refresh-ahead / stale-while-revalidate / stale-if-error / negative caching /
// TTL jitter, a SetNX-based distributed Locker, namespace prefixing,
// pluggable codecs, and zero-dependency observability hooks.
//
// The core package has zero third-party dependencies. Backends live in sibling
// modules that implement the [Cache] interface (github.com/ubgo/cache-mem,
// cache-redis, cache-pg, cache-tiered). Every backend is validated by the
// shared conformance suite in github.com/ubgo/cache/cachetest.
//
// Bytes in, bytes out:
//
//	c := memcache.New()                      // any Cache implementation
//	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
//	b, err := c.Get(ctx, "k")                // (nil, ErrNotFound) on miss
//
// Typed + load-through:
//
//	u, err := cache.Remember(ctx, c, "user:42", 5*time.Minute,
//	    func(ctx context.Context) (User, error) { return db.LoadUser(ctx, 42) },
//	    cache.WithJitter(0.1),
//	    cache.WithStaleIfError(time.Minute),
//	)
//
// Get MUST return (nil, ErrNotFound) on a miss — never (nil, nil).
package cache
