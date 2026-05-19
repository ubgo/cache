# Migrating to ubgo/cache

Coming from `dgraph-io/ristretto`, `hashicorp/golang-lru`, `patrickmn/go-cache`,
or a hand-rolled `map+sync.RWMutex`? This maps the concepts and shows the
before/after. The big shift: ubgo/cache separates the **contract** (the
`cache.Cache` interface) from the **backend** (mem/redis/pg/…), so the same
call sites work whether the cache is in-process or distributed.

## Concept mapping

| You had | ubgo/cache equivalent |
|---|---|
| ristretto `cache.Get/Set/Del` (`interface{}`) | `cache.Get/Set/Del` (bytes) + `cache.GetT[T]/SetT[T]` for typed values |
| ristretto cost / `MaxCost` | `memcache.WithMaxBytes` + `memcache.WithWeigher` |
| ristretto admission (TinyLFU) | `memcache.AdaptiveWTinyLFU` (the default policy) |
| golang-lru `lru.New(size)` | `memcache.New(memcache.WithMaxEntries(size), memcache.WithPolicy(memcache.LRU))` |
| golang-lru `expirable.NewLRU(size, nil, ttl)` | `memcache.WithMaxEntries(size)` + per-entry TTL on `Set` |
| go-cache `New(defaultTTL, cleanup)` | `memcache.New(memcache.WithSweepInterval(cleanup))`, TTL passed per `Set` |
| go-cache `SetDefault/Get` | `cache.Set(ctx,k,v,ttl)` / `cache.Get` |
| singleflight + cache by hand | `cache.Remember(ctx, c, key, ttl, loadFn)` |
| manual "refresh before expiry" | `cache.WithRefreshAhead(0.8)` |
| serve-stale-on-error glue | `cache.WithStaleIfError(d)` / `cache.WithStaleWhileRevalidate(d)` |
| negative-result caching glue | `cache.WithNegativeTTL(d)` |
| swap mem→redis later (big rewrite) | change one constructor; call sites unchanged |

## golang-lru → ubgo/cache

```go
// before
l, _ := lru.New[string, []byte](1024)
l.Add("k", v)
got, ok := l.Get("k")

// after
c := memcache.New(memcache.WithMaxEntries(1024), memcache.WithPolicy(memcache.LRU))
defer c.Close()
_ = c.Set(ctx, "k", v, 0)            // 0 = no expiry
got, err := c.Get(ctx, "k")          // err == cache.ErrNotFound on miss
```

Want better hit rates with no other change? Drop `WithPolicy(memcache.LRU)` —
the default `AdaptiveWTinyLFU` typically beats LRU by 5–15 points on skewed and
scan-heavy traffic (see `docs/testing.md` for the harness).

## ristretto → ubgo/cache

```go
// before
rc, _ := ristretto.NewCache(&ristretto.Config{NumCounters: 1e7, MaxCost: 1<<30, BufferItems: 64})
rc.Set("k", v, 1); rc.Wait()
got, ok := rc.Get("k")

// after
c := memcache.New(
    memcache.WithMaxBytes(1<<30),
    memcache.WithWeigher(func(b []byte) int64 { return int64(len(b)) }),
)
defer c.Close()
_ = c.Set(ctx, "k", v, 0)            // synchronous; no Wait() needed
got, err := c.Get(ctx, "k")
```

Notes: ubgo/cache `Set` is synchronous and immediately visible (no
write-buffer/`Wait()`). Eviction is W-TinyLFU like ristretto, but the cost
function is `WithWeigher`. Typed values: use `cache.GetT[T]/SetT[T]` or
`cache.NewTyped[T]` instead of storing `interface{}`.

## go-cache → ubgo/cache

```go
// before
gc := gocache.New(5*time.Minute, 10*time.Minute)
gc.Set("k", v, gocache.DefaultExpiration)
x, found := gc.Get("k")

// after
c := memcache.New(memcache.WithSweepInterval(10 * time.Minute))
defer c.Close()
_ = c.Set(ctx, "k", v, 5*time.Minute)
b, err := c.Get(ctx, "k")            // found == (err == nil)
```

## Gotchas

- **Miss is an error, not `ok=false`.** `Get` returns `cache.ErrNotFound`; check with `errors.Is(err, cache.ErrNotFound)`. This makes a real backend outage distinguishable from a miss (it returns a different error) — something `ok bool` APIs cannot express.
- **Bytes core.** The interface stores `[]byte`. Use `GetT/SetT/Remember/Typed[T]` for typed values; pick the codec with `cache.WithCodec` (JSON default; Gob/Raw built in; msgpack/zstd/protobuf in contrib).
- **`ttl <= 0` means no expiry** (not "expire immediately").
- **Always `defer c.Close()`** on `cache-mem` — it stops the sweeper/checkpoint/AOF goroutines.
- **Concurrency**: every backend is safe for concurrent use; no external locking needed.
- **Switching backend is a one-liner.** Start with `cache-mem`; move to `cache-redis` or `cache-tiered` later without touching call sites — they all satisfy `cache.Cache` and pass the same conformance suite.

See [`docs/README.md`](./README.md) for the full per-feature cookbook and
[`docs/family.md`](./family.md) for choosing a backend.
