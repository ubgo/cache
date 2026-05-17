# Core operations — the `Cache` interface

`cache.Cache` is the bytes-in/bytes-out contract every backend implements. It
is intentionally untyped; typed ergonomics live in [`Remember`](./remember.md)
and [`Typed[T]`](./generics.md). Every method below is enforced by the
[conformance suite](./testing.md), so behavior is identical across adapters.

```go
import (
	"context"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)

func newCache() cache.Cache { return mem.New() } // any adapter
```

---

### `Get(ctx, key) ([]byte, error)`

Read one key. Returns `(nil, ErrNotFound)` on a miss or expiry — never
`(nil, nil)`.

Use cases:
- Read a serialized session blob you wrote with `Set`.
- Probe-and-branch: treat `ErrNotFound` as "compute it".

```go
c := newCache()
ctx := context.Background()
b, err := c.Get(ctx, "session:abc")
if errors.Is(err, cache.ErrNotFound) {
	// cache miss — load from source
} else if err != nil {
	// backend error
} else {
	use(b)
}
```

### `GetMulti(ctx, keys) (map[string][]byte, error)`

Batch read. Absent keys are simply omitted from the result map (no error per
missing key).

Use cases:
- Hydrate a list view (50 product cards) in one round-trip.
- Partition hits vs misses for a batch loader.

```go
got, err := c.GetMulti(ctx, []string{"p:1", "p:2", "p:3"})
for _, id := range []string{"p:1", "p:2", "p:3"} {
	if v, ok := got[id]; ok {
		render(id, v)
	} else {
		missing = append(missing, id)
	}
}
```

### `Has(ctx, key) (bool, error)`

Existence check without transferring the value.

Use cases:
- Rate-limit gate: "has this IP a `seen:` marker?"
- Cheap presence check before an expensive `Get` of a large blob.

```go
if ok, _ := c.Has(ctx, "rl:"+ip); ok {
	return errTooMany
}
```

### `TTL(ctx, key) (time.Duration, error)`

Remaining lifetime. A non-positive duration means "no expiry". `ErrNotFound`
if absent.

Use cases:
- Show "cached, refreshes in 42s" in a debug header.
- Decide whether to proactively refresh before serving.

```go
d, err := c.TTL(ctx, "feed:home")
if err == nil && d > 0 && d < 10*time.Second {
	go refreshFeed()
}
```

### `Set(ctx, key, val, ttl) error`

Write/overwrite a key. `ttl <= 0` means "no expiry" (lives until evicted or
explicitly deleted).

Use cases:
- Cache a rendered HTML fragment for 5 minutes.
- Store a config blob with no TTL, invalidated explicitly on change.

```go
_ = c.Set(ctx, "frag:nav", html, 5*time.Minute)
_ = c.Set(ctx, "config:flags", blob, 0) // permanent until Del
```

### `SetMulti(ctx, items) error`

Batch write, each entry carrying its own TTL and optional `Tags`.

Use cases:
- Write back a page of DB rows after a bulk load.
- Seed several derived keys atomically-ish in one call.

```go
_ = c.SetMulti(ctx, map[string]cache.Item{
	"p:1": {Value: b1, TTL: time.Minute},
	"p:2": {Value: b2, TTL: time.Minute, Tags: []string{"catalog"}},
})
```

### `SetNX(ctx, key, val, ttl) (bool, error)`

Set only if absent. Returns `(true, nil)` only when it created the key. This is
the primitive [`Locker`](./locker.md) and [`Once`](./helpers.md) are built on.

Use cases:
- Leader election / "only one pod runs the nightly job".
- One-time initialization marker.

```go
created, _ := c.SetNX(ctx, "init:done", []byte{1}, time.Hour)
if created {
	runOneTimeSetup()
}
```

### `Expire(ctx, key, ttl) error`

Reset the TTL of an existing key.

Use cases:
- Sliding-window session: extend on every authenticated request.
- Promote a key to permanent (`ttl <= 0`).

```go
_ = c.Expire(ctx, "session:abc", 30*time.Minute) // sliding expiry
```

### `Touch(ctx, key) error`

Bump a key's lifetime by the adapter's default extension without rewriting the
value.

Use cases:
- Keep-alive for a key you just confirmed is still relevant.
- Cheap "mark recently used" without a value round-trip.

```go
_ = c.Touch(ctx, "session:abc")
```

### `Incr(ctx, key, delta) (int64, error)` / `Decr(ctx, key, delta) (int64, error)`

Atomic counters. `delta` may be negative; a missing key starts at 0. `Decr`
is `Incr` with a negated delta.

Use cases:
- Per-minute rate-limit counters.
- Inventory decrement, page-view tallies, feature-flag rollouts.

```go
n, _ := c.Incr(ctx, "rl:"+ip+":"+minute, 1)
if n == 1 {
	_ = c.Expire(ctx, "rl:"+ip+":"+minute, time.Minute)
}
if n > 100 {
	return errRateLimited
}
remaining, _ := c.Decr(ctx, "stock:sku42", 1)
```

### `Del(ctx, keys...) error`

Delete one or more keys. No error if a key is already absent.

Use cases:
- Invalidate on write ("user updated → drop `user:42`").
- Bulk purge a known key set.

```go
_ = c.Del(ctx, "user:42", "user:42:profile")
```

### `DeleteByPrefix(ctx, prefix) error`

Delete every key under a prefix.

Use cases:
- Drop all of one tenant's cached data on offboarding.
- Invalidate a whole feature namespace after a schema change.

```go
_ = c.DeleteByPrefix(ctx, "tenant:acme:")
```

### `Flush(ctx) error`

Clear everything the cache can see. (On a [namespaced](./namespacing.md) view
this scopes to the prefix.)

Use cases:
- Test teardown.
- Emergency "blow away poisoned cache" admin action.

```go
_ = c.Flush(ctx)
```

### `Iterate(ctx, opts) Iterator`

Forward-only scan. Adapters that cannot scan return an iterator whose first
`Next()` is `false` and whose `Err()` is `ErrUnsupported`.

Use cases:
- Export/debug dump of all `user:` keys.
- Maintenance sweep that re-evaluates entries.

```go
it := c.Iterate(ctx, cache.IterateOpts{Prefix: "user:", Count: 500})
defer it.Close()
for it.Next() {
	fmt.Println(it.Key(), len(it.Value()))
}
if err := it.Err(); err != nil && !errors.Is(err, cache.ErrUnsupported) {
	log.Fatal(err)
}
```

### `Ping(ctx) error`

Backend health check.

Use cases:
- Kubernetes readiness/liveness probe.
- Startup gate before serving traffic.

```go
if err := c.Ping(ctx); err != nil {
	http.Error(w, "cache down", http.StatusServiceUnavailable)
}
```

### `Close() error`

Release backend resources. Must be idempotent (a second `Close` is a no-op).

Use cases:
- Graceful shutdown.
- `defer c.Close()` in short-lived jobs.

```go
c := newCache()
defer c.Close()
```

### `Stats() Stats`

Point-in-time snapshot. Fields an adapter does not track stay zero.

```go
s := c.Stats()
log.Printf("hit ratio %.2f, entries %d", s.HitRatio(), s.Entries)
```

---

## `Item`

Value plus per-entry TTL and optional tags, used by `SetMulti`.

```go
type Item struct {
	Value []byte
	TTL   time.Duration
	Tags  []string // adapters without tag support ignore it
}
```

Use case: heterogeneous batch write where each value has a different lifetime.

## `IterateOpts`

Controls `Iterate`. `Prefix: ""` iterates everything; `Count` is a batch-size
hint (0 = adapter default).

```go
opts := cache.IterateOpts{Prefix: "order:", Count: 1000}
```

## `Iterator`

Forward-only cursor — always `Close` it.

```go
type Iterator interface {
	Next() bool
	Key() string
	Value() []byte
	Err() error
	Close() error
}
```

---

## Sentinel errors

`errors.Is`-comparable regardless of backend; adapters may wrap them with `%w`.

| Error | Triggered when |
|---|---|
| `ErrNotFound` | `Get`/`GetMulti`/`TTL` on an absent or expired key. `Get` MUST return `(nil, ErrNotFound)`, never `(nil, nil)`. |
| `ErrUnsupported` | An optional op the adapter cannot serve (e.g. `Iterate` on a non-scannable backend). |
| `ErrSerialization` | A codec encode/decode failure (bad JSON, gob mismatch, `RawCodec` misuse, tampered ciphertext). |
| `ErrTimeout` | Operation exceeded its context deadline and the adapter surfaces it as a typed error (also returned by [`NewBulkhead`](./resilience.md) when a slot can't be acquired and `ctx.Err()` is nil). |
| `ErrCircuitOpen` | Returned by [`NewCircuitBreaker`](./resilience.md) while the breaker is open. |
| `ErrTooLarge` | A value exceeds the adapter's max value size. |
| `ErrKeyTooLong` | A key exceeds the adapter's max key length. |
| `ErrClosed` | Any operation invoked after `Close` (e.g. on `cachetest.Mock`). |

```go
switch {
case errors.Is(err, cache.ErrNotFound):
	// miss — compute
case errors.Is(err, cache.ErrCircuitOpen), errors.Is(err, cache.ErrTimeout):
	// degrade gracefully — serve a default
case errors.Is(err, cache.ErrSerialization):
	// poisoned entry — log and treat as miss
}
```

---

## `Stats`, `EvictionCause`, `HitRatio`

`Stats` is an adapter-reported snapshot. Counters are cumulative since process
start; `Entries`/`Bytes` are instantaneous gauges.

```go
type Stats struct {
	Hits, Misses, Sets, Deletes, Evictions int64
	EvictionsByCause map[EvictionCause]int64 // may be nil
	Entries, Bytes   int64
}
```

`EvictionCause` classifies why an entry left:

| Constant | Meaning |
|---|---|
| `EvictSize` | capacity (max entries / max bytes) |
| `EvictExpired` | TTL elapsed |
| `EvictExplicit` | `Del` / `Flush` / `DeleteByPrefix` |
| `EvictReplaced` | overwritten by a new `Set` |

`HitRatio()` is `hits / (hits + misses)`, returning `0` when there has been no
traffic.

Use cases:
- Emit `cache_hit_ratio` to your metrics system on a ticker.
- Alert when `EvictionsByCause[EvictSize]` spikes (cache too small).

```go
s := c.Stats()
fmt.Printf("hit-ratio=%.3f size-evictions=%d\n",
	s.HitRatio(), s.EvictionsByCause[cache.EvictSize])
```
