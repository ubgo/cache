# Namespacing — `Namespaced`

### `Namespaced(c Cache, prefix string) Cache`

One-line: a `Cache` view that transparently prefixes every key (a trailing
`":"` is added if absent), so many services/tenants share one backend without
key collisions.

It rewrites keys on every operation (`Get`, `Set`, `GetMulti`, `SetMulti`,
`Del`, `DeleteByPrefix`, `Incr`, `Iterate`, …) and strips the prefix back off
on read paths (`GetMulti` keys, `Iterator.Key()`), so callers only ever see
unprefixed keys.

Scoped `Flush`: on a namespaced view, `Flush` calls `DeleteByPrefix(prefix)`
instead of wiping the whole backend — you can clear *your* namespace without
nuking a neighbor's data. (An empty prefix falls back to a real `Flush`.)

Use cases:
- One Redis shared by `billing`, `search`, `auth` services without coordinating key naming.
- Per-tenant isolation in a multi-tenant app.
- Safely `Flush` only your feature's keys.

```go
import (
	"context"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)

func example() {
	ctx := context.Background()
	backend := mem.New() // shared by everyone

	billing := cache.Namespaced(backend, "svc:billing") // -> "svc:billing:"
	search  := cache.Namespaced(backend, "svc:search")

	_ = billing.Set(ctx, "invoice:7", []byte("…"), time.Minute) // stored as svc:billing:invoice:7
	_ = search.Set(ctx, "invoice:7", []byte("…"), time.Minute)  // distinct: svc:search:invoice:7

	// Caller sees the unprefixed key:
	b, _ := billing.Get(ctx, "invoice:7")
	_ = b

	// Clears ONLY svc:billing:* — search data untouched:
	_ = billing.Flush(ctx)
}
```

Namespaced views compose with every decorator — wrap the namespaced view with
[`Instrument`](./observability.md) and the `ObsHooks.Namespace` label lines up
with the prefix, or nest namespaces (`Namespaced(Namespaced(b, "tenant:acme"),
"svc:billing")`).
