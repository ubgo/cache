# Cross-process invalidation — `Invalidation`

A best-effort fan-out of "these keys changed" events across processes. A
tiered/L1 cache subscribes so it can drop locally-cached copies when another
node mutates a key. Delivery is best-effort: a missed message only means a
stale L1 entry until its own (short) TTL elapses.

```go
import (
	"context"

	"github.com/ubgo/cache"
)
```

---

### `Invalidation` interface

```go
type Invalidation interface {
	Publish(ctx context.Context, keys ...string) error          // safe for concurrent use
	Subscribe(ctx context.Context, fn func(key string)) error   // blocks until ctx done
}
```

`Subscribe` blocks until `ctx` is cancelled — run it in its own goroutine. The
in-process implementation is the reference semantics every distributed
implementation (e.g. Redis Pub/Sub in `cache-redis`) must match.

### `NewInProcInvalidation() Invalidation`

One-line: an in-process invalidation bus (single binary / tests), and the
reference behavior for distributed buses.

Use cases:
- Single-binary app with multiple in-memory caches that must agree.
- Unit-testing invalidation wiring without standing up Redis.
- The L1 drop-on-change mechanism behind a tiered cache.

```go
bus := cache.NewInProcInvalidation()

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
	_ = bus.Subscribe(ctx, func(key string) {
		if key == cache.InvalidateAll {
			l1.Flush(ctx) // drop everything
			return
		}
		_ = l1.Del(ctx, key) // drop the one stale local copy
	})
}()

// After mutating the source of truth and the shared backend:
_ = bus.Publish(ctx, "user:42")
```

### `InvalidateAll`

`const InvalidateAll = ""` — the broadcast sentinel. A subscriber receiving the
empty key should drop its entire local view.

Use cases: schema change, mass config flip, "nuke all L1 caches now".

```go
_ = bus.Publish(ctx, cache.InvalidateAll) // every subscriber flushes its L1
```
