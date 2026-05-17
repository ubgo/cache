# Distributed lock — `Locker`

A portable distributed lock built on the `Cache.SetNX` primitive. Works on any
adapter with no backend-specific code.

```go
import (
	"context"
	"errors"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)
```

---

### `NewLock(c Cache, key string, ttl time.Duration) Locker`

One-line: a lease-based mutex for `key` with lease lifetime `ttl`. Each
`Locker` carries a random 16-byte token written as the lock value; the lock key
is internally prefixed (`__lock__:`) so it never collides with data keys.

Use cases:
- "Only one pod runs the nightly billing job."
- Serialize a non-idempotent migration / cache rebuild across replicas.
- Leader election for a singleton background worker.

```go
l := cache.NewLock(mem.New(), "cron:nightly-billing", 30*time.Second)
```

### `Locker.Acquire(ctx) error`

Creates the lock via `SetNX`. Returns `ErrLockNotAcquired` if it is already
held by someone else; any backend error is returned as-is.

```go
ctx := context.Background()
if err := l.Acquire(ctx); errors.Is(err, cache.ErrLockNotAcquired) {
	return // another pod owns it — skip this run
} else if err != nil {
	return // backend error
}
defer l.Release(ctx)
runNightlyBilling(ctx)
```

### `Locker.Refresh(ctx) error`

Extends the TTL **iff this holder still owns the lock** (token-checked, then
`Expire`). Returns `ErrLockNotAcquired` if ownership was lost.

Use cases: keep a long critical section's lease alive — heartbeat the lock
faster than the TTL.

```go
go func() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		if err := l.Refresh(ctx); err != nil {
			return // lost the lock — abort the work
		}
	}
}()
```

### `Locker.Release(ctx) error`

Frees the lock **iff this holder still owns it**. If the lease already expired
and was re-taken by someone else, `Release` is a safe no-op (it will not delete
the new owner's lock).

```go
defer l.Release(ctx)
```

### `ErrLockNotAcquired`

`var ErrLockNotAcquired = errors.New("cache: lock not acquired")`. Returned by
`Acquire` (already held) and `Refresh` (ownership lost). Compare with
`errors.Is`.

---

## Token safety model & limitations

`Refresh`/`Release` re-read the stored value and proceed only if it still
matches this holder's token, so a holder whose lease already expired (and was
re-acquired by someone else) cannot stomp the new owner. This is
**check-then-act, not atomic compare-and-delete**: it closes the common
"released someone else's lock" race but is **not** a substitute for a fencing
token in systems that require strict mutual exclusion under arbitrary process
pauses (GC, VM freeze). Mitigation: use a short-enough `ttl` plus periodic
`Refresh` for long critical sections, and make the protected work idempotent
where possible.
