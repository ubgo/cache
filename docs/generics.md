# Generics — `Typed[T]`

A `Typed[T]` is a generics wrapper so a whole module works with `T` instead of
`[]byte`. It carries a default codec and a fixed set of `RememberOpt`s applied
to every call, so option boilerplate lives in one place.

```go
import (
	"context"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)

type User struct{ ID int; Name string }
```

---

> [!IMPORTANT]
> **Cache serializable data, not live objects.** Every `Typed[T]` operation runs `T` through a codec: encode on `Set`, decode on every `Get`. So you never get the *same* object back — you get a freshly decoded copy. That is fine for plain data (`User{ID, Name}`, a config struct, a `[]Result`), but it silently breaks values whose usefulness depends on staying live: anything with unexported fields or bindings that do not round-trip — a `*regexp.Regexp`, `*http.Client`, an open connection, a `func`, a `chan`, or an **ORM entity** like an ent `*ent.User`. An ent entity carries an unexported client handle; after a cache round-trip that handle is nil, so the decoded copy reads its scalar fields fine but panics the moment anyone calls `.QueryEdges()` / `.Update()` on it. The trap is that it looks like it works (scalar reads pass) until a future caller relies on the liveness. **Cache a flat DTO with just the fields you need, not the raw entity** — then there is no liveness to lose. If you genuinely need to hold a live object by reference with zero serialization, a cache is the wrong tool: use a `sync.Map` or a small typed helper instead.

### `NewTyped[T](c Cache, opts ...RememberOpt) *Typed[T]`

One-line: wrap a byte cache into a type-safe handle; `opts` apply to every
`Get`/`Set`/`Remember` on this view.

Use cases:
- A package-level `var userCache = cache.NewTyped[User](backend, …)` so call sites never repeat codec/jitter options.
- One backend, several typed views (`Typed[User]`, `Typed[Order]`) sharing a keyspace.

```go
users := cache.NewTyped[User](mem.New(),
	cache.WithJitter(0.1),
	cache.WithStaleIfError(time.Minute),
)
```

### `(*Typed[T]) Get(ctx, key) (T, error)`

Read + decode the typed value (delegates to `GetT[T]` with the baked options).
Returns `cache.ErrNotFound` on miss.

```go
u, err := users.Get(ctx, "user:42")
```

### `(*Typed[T]) Set(ctx, key, v, ttl) error`

Encode + store `v` (delegates to `SetT[T]`; plain codec payload, no envelope).

```go
_ = users.Set(ctx, "user:42", User{ID: 42, Name: "Ada"}, time.Minute)
```

### `(*Typed[T]) Remember(ctx, key, ttl, fn) (T, error)`

Cached value or single-flight `fn` on miss — the [`Remember`](./remember.md)
workhorse with the view's options pre-applied.

Use cases: the everyday read path in a typed service layer.

```go
u, err := users.Remember(ctx, "user:42", 5*time.Minute,
	func(ctx context.Context) (User, error) { return db.User(ctx, 42) })
```

### `(*Typed[T]) Del(ctx, keys...) error`

Remove keys (delegates straight to the underlying cache).

Use cases: invalidate-on-write from the same typed handle.

```go
_ = users.Del(ctx, "user:42")
```

### `(*Typed[T]) Raw() Cache`

Return the underlying bytes-level cache for ops the typed view does not expose
(`Has`, `TTL`, `Incr`, `Iterate`, …) or to share one backend between a typed
view and raw access. Same keyspace: a key written via `Set` here is readable
via `Raw().Get` (as a codec payload — decode accordingly).

Use cases:
- Run a counter (`Incr`) or existence check (`Has`) alongside typed reads.
- Mount the [admin endpoint](./admin.md) or wrap with [resilience](./resilience.md) on the raw cache.

```go
raw := users.Raw()
ok, _ := raw.Has(ctx, "user:42")
n, _ := raw.Incr(ctx, "user:42:views", 1)
_ = ok
_ = n
```
