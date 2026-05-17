# Helpers — memoize, idempotency, warmup, keys

```go
import (
	"context"
	"errors"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)

type User struct{ ID int; Name string }
type Product struct{ ID string }
```

---

### `Memoize[K, V](c, prefix, ttl, fn, keyPart, opts...) func(ctx, K) (V, error)`

One-line: wrap a pure-ish function with cache-backed single-flight memoization;
the returned function caches each distinct argument's result for `ttl`.
`keyPart` must render the argument to a stable string (namespaced under
`prefix`). Concurrent calls for the same argument collapse to one load (built
on [`Remember`](./remember.md)).

Use cases: turn `db.User(ctx, id)` into a cached `getUser(ctx, id)` with one
line; memoize an expensive pure computation keyed by its input.

```go
getUser := cache.Memoize(mem.New(), "user", time.Minute,
	func(ctx context.Context, id int) (User, error) { return db.User(ctx, id) },
	func(id int) string { return fmt.Sprint(id) },
)
u, err := getUser(context.Background(), 42) // DB hit once; then cached
_ = u
_ = err
```

### `QueryKey(name string, parts ...any) string`

One-line: a deterministic, collision-resistant cache key from a logical name
plus arbitrary parts. Parts are length-prefixed before hashing, so `("a","bc")`
and `("ab","c")` never collide.

Use cases: key a cached query by `(sql, bound args)`; key a paginated/filtered
result set; build the `keyPart` for `Memoize` over a composite argument.

```go
key := cache.QueryKey("users.byOrg", orgID, page, sortField)
rows, err := cache.Remember(ctx, c, key, time.Minute,
	func(ctx context.Context) ([]User, error) { return db.UsersByOrg(ctx, orgID, page, sortField) })
_ = rows
_ = err
```

### `ErrInFlight`

`var ErrInFlight = errors.New("cache: idempotent operation already in flight")`.
Returned by `Once` when another caller currently holds the idempotency lease
and no result is cached yet — the caller may retry shortly.

### `Once[T](ctx, c, key, ttl, fn, opts...) (T, error)`

One-line: run `fn` at most once per idempotency `key` and cache its typed
result for `ttl`, so a retried request returns the original result instead of
executing again.

- First caller: acquires a `SetNX` lease, runs `fn`, caches the result.
- Concurrent caller before the result is stored: gets `ErrInFlight`.
- Later caller within `ttl`: gets the cached result without running `fn`.
- If `fn` fails, the lease is released and the failure is **not** cached — a transient failure stays retryable.

Use cases: deduplicate a double-clicked payment; make a webhook handler
idempotent against provider retries; one-shot side effects under at-least-once
delivery.

```go
res, err := cache.Once(ctx, c, "charge:"+idemKey, 24*time.Hour,
	func(ctx context.Context) (ChargeResult, error) {
		return stripe.Charge(ctx, amount) // executes exactly once per idemKey
	})
if errors.Is(err, cache.ErrInFlight) {
	// another worker is processing the same request — 409 / retry later
}
_ = res
```

### `WarmResult`

`type WarmResult struct { Key string; Err error }` — outcome of one key's
warmup. `Err` is nil on success, or `ErrNotFound` if the loader had no value.

### `Warm[T](ctx, c, keys, ttl, concurrency, load, opts...) []WarmResult`

One-line: preload `keys` into `c` before traffic arrives (startup, post-deploy)
so the first users don't all stampede a cold cache. Loads run with bounded
concurrency; a per-key loader failure is reported in the result, not fatal, so
one bad key can't abort the warmup. Honors `ctx` cancellation (a cancelled
warmup marks every remaining key with `ctx.Err()`). Values are stored with
`SetT` semantics (plain codec) — read back with `GetT`/`Remember`.
`concurrency < 1` is treated as 1.

Use cases: warm the top-N hot products at boot; rebuild a cache after a deploy
or flush; pre-fill before a known traffic spike (sale launch).

```go
hotIDs := topProductIDs() // e.g. from yesterday's HotKeyTracker
res := cache.Warm(ctx, c, hotIDs, 5*time.Minute, 16,
	func(ctx context.Context, id string) (Product, error) {
		return db.Product(ctx, id)
	})
for _, r := range res {
	if r.Err != nil {
		log.Printf("warm %s failed: %v", r.Key, r.Err)
	}
}
```
