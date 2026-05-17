# Testing & tuning — `github.com/ubgo/cache/cachetest`

The shared conformance suite every adapter runs, a correct in-memory `Mock` for
consumer unit tests, and a Zipfian/hit-rate harness for tuning eviction
policies.

```go
import (
	"context"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	mem "github.com/ubgo/cache-mem"
)
```

---

### `cachetest.Factory`

`type Factory func(t *testing.T) cache.Cache` — builds a fresh, empty cache for
one subtest.

### `cachetest.Run(t *testing.T, factory Factory)`

One-line: execute the full conformance suite against the adapter built by
`factory` — the single source of truth for "does this backend behave like a
`cache.Cache`".

It asserts: miss → `ErrNotFound`, set/get roundtrip, overwrite, TTL expiry,
no-TTL persistence, `Has`, multi-ops, `SetNX` first-wins, `Expire`, concurrent
`Incr`/`Decr` correctness, `DeleteByPrefix` scoping, `Flush`, prefix-scoped
iteration (skips if `ErrUnsupported`), `Ping`, idempotent `Close`, `Remember`
single-flight, and `Locker` mutual exclusion.

Use case (every adapter author wires it from one `_test.go`):

```go
func TestConformance(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Cache {
		return mem.New() // a fresh, empty instance per subtest
	})
}
```

### `cachetest.Bench(b *testing.B, factory func(b *testing.B) cache.Cache)`

One-line: comparable benchmarks across adapters (`Set`, `GetHot`, `GetCold`,
`Remember`) so you can fairly compare backends.

Use cases: regression-guard adapter performance; compare mem vs redis vs tiered
on the same workloads.

```go
func BenchmarkAdapter(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache { return mem.New() })
}
```

---

## `Mock` — for consumer unit tests

A correct, dependency-free in-memory `cache.Cache`. It passes the conformance
suite, so it is also the reference implementation.

### `cachetest.NewMock() *Mock`

One-line: an empty in-memory cache for testing code that depends on a
`cache.Cache` — no Redis, no goroutines, deterministic.

Use cases: unit-test a service that takes a `cache.Cache`; assert your
load-through logic without a real backend.

```go
func TestUserService(t *testing.T) {
	c := cachetest.NewMock()
	svc := NewUserService(c)
	if _, err := svc.Get(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
}
```

### `(*Mock) FailOn` field

`FailOn map[string]error` — inject an error from every op whose lowercased name
matches (`"get"`, `"set"`, `"setnx"`, `"del"`, `"getmulti"`, `"ping"`, …).

Use cases: test your error/degradation paths — circuit-breaker fallback,
stale-if-error, retry behavior.

```go
m := cachetest.NewMock()
m.FailOn = map[string]error{"get": errors.New("boom")}
// now exercise the code path that must survive a failing Get
```

### `(*Mock) SetClock(fn func() time.Time)`

Override the clock for deterministic TTL tests — advance time without
`time.Sleep`.

Use cases: assert TTL expiry, refresh-ahead thresholds, negative-cache windows
instantly and flake-free.

```go
m := cachetest.NewMock()
now := time.Now()
m.SetClock(func() time.Time { return now })
_ = m.Set(context.Background(), "k", []byte("v"), time.Minute)
now = now.Add(2 * time.Minute)               // jump past TTL
_, err := m.Get(context.Background(), "k")   // cache.ErrNotFound
_ = err
```

(`Mock` also implements every `cache.Cache` method incl. `Iterate`/`Stats`;
`Stats` reports the live entry count. `Close` flips it to return
`cache.ErrClosed`.)

---

## Eviction-policy tuning harness

### `cachetest.Zipfian` / `NewZipfian(n int, s float64, seed int64) *Zipfian`

One-line: a key generator following a Zipf distribution (a few very hot keys, a
long cold tail) — the shape of almost all real cache traffic. `s > 1` skews
harder toward the hot set; `seed` makes runs reproducible. `(*Zipfian).Key()`
returns the next key (e.g. `"k:00000123"`).

Use cases: realistic load when measuring an LRU/W-TinyLFU adapter's hit-rate.

```go
z := cachetest.NewZipfian(100_000, 1.07, 42)
hr := cachetest.HitRate(mem.New(), z.Key, 1_000_000)
```

### `cachetest.ScanGen(hotSize, hotEvery int, seed int64) func() string`

One-line: a generator interleaving a small repeatedly-accessed hot set with a
long stream of unique one-shot "scan" keys — the classic pattern that flushes a
naive LRU. `hotEvery` = 1-in-N accesses hit the hot set.

Use cases: prove an adapter is scan-resistant (W-TinyLFU should keep the hot
set; LRU should collapse).

```go
gen := cachetest.ScanGen(1000, 5, 1) // 1-in-5 accesses are hot
hr := cachetest.HitRate(c, gen, 500_000)
```

### `cachetest.HitRate(c cache.Cache, gen func() string, ops int) float64`

One-line: replays `ops` accesses against `c` read-through (a miss stores the
key), returning the observed hit ratio in `[0,1]` — measures how well the
eviction policy retains the hot set under capacity pressure.

```go
hr := cachetest.HitRate(c, cachetest.NewZipfian(50_000, 1.2, 7).Key, 1_000_000)
```

### `cachetest.Round(x float64) float64`

One-line: round to 4 decimals for stable benchmark/test reporting.

```go
fmt.Println(cachetest.Round(hr)) // e.g. 0.8421
```

End-to-end tuning example:

```go
func TestScanResistant(t *testing.T) {
	c := mem.New() // small-capacity instance
	hr := cachetest.Round(cachetest.HitRate(c, cachetest.ScanGen(1000, 5, 1), 200_000))
	if hr < 0.15 {
		t.Fatalf("eviction policy not scan-resistant: hit-rate %.4f", hr)
	}
}
```
