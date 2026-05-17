# Observability

Zero-dependency seams: the core never imports OpenTelemetry or Prometheus.
Wrap a cache to get per-op events, per-namespace hit-rates, and hot-key
detection; the `contrib/cache-otel` and `contrib/cache-prom` modules implement
the callbacks.

```go
import (
	"context"
	"log"
	"time"

	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)
```

---

### `Instrument(c Cache, hooks ObsHooks) Cache`

One-line: wrap `c` so every `Get`/`Set`/`Del` reports through `hooks` and
updates an internal `Stats` counter set (merged on top of whatever the adapter
already reports).

Only `Get`/`Set`/`Del` are instrumented — the dominant traffic and the ops
whose hit/miss outcome matters for SLOs. Other ops pass straight through to
keep the hot path cheap; wrap at the adapter level or use the contrib exporters
if you need every op traced. A zero `ObsHooks` gives counters-only with no
exporter.

Use cases: emit metrics/traces without adding a dependency; SLO hit-ratio;
slow-op logging.

```go
c := cache.Instrument(mem.New(), cache.ObsHooks{
	Adapter:   "mem",
	Namespace: "billing",
	OnOp: func(ev cache.OpEvent) {
		if ev.Duration > 50*time.Millisecond {
			log.Printf("slow %s %s %v", ev.Op, ev.KeyHash, ev.Duration)
		}
	},
	OnHit:  func(h string) { metrics.Inc("cache.hit") },
	OnMiss: func(h string) { metrics.Inc("cache.miss") },
})
s := c.Stats() // includes wrapper-observed hits/misses/sets/deletes
_ = s
```

### `ObsHooks`

Optional callbacks + labels attached to every event.

```go
type ObsHooks struct {
	Adapter   string                 // label
	Namespace string                 // label
	OnOp      func(ev OpEvent)       // after every instrumented op
	OnHit     func(keyHash string)   // read hit (hash, never raw key)
	OnMiss    func(keyHash string)   // read miss
}
```

### `OpEvent`

One completed operation. `KeyHash` is privacy-safe (raw keys may contain PII
and are never emitted).

```go
type OpEvent struct {
	Op, Adapter, Namespace, KeyHash string
	Hit       bool
	Err       error
	Duration  time.Duration
}
```

Use case: drive a per-op latency histogram and an error counter.

### `KeyHash(key string) string`

The privacy-safe key identifier: first 8 bytes of `sha256(key)`, hex-encoded.
Used internally by `Instrument`; exported so your own logs/spans use the same
identifier.

```go
log.Printf("evicting %s", cache.KeyHash("user:42")) // stable, non-PII
```

---

## Hot-key detection — `HotKeyTracker`

### `NewHotKeyTracker(c Cache, capacity int) *HotKeyTracker`

One-line: wraps `c` and approximates the most frequently **read** keys using
the Space-Saving algorithm — a fixed pool of `capacity` counters, O(1) per
access, bounded memory regardless of keyspace size. It is itself a `Cache`
(`Get`/`GetMulti` record the access then delegate; `TTL` and other ops pass
through untracked). `capacity < 1` is treated as 1.

Counts are approximate (over-estimate by at most the evicted minimum); the
*ranking* of genuinely hot keys is reliable, exact counts are not.

Use cases: find the single key that is 60% of your traffic (a hotspot you
otherwise cannot see); decide what to pin/replicate.

```go
hk := cache.NewHotKeyTracker(mem.New(), 100)
c := hk // use as a normal cache.Cache everywhere
_ = c
```

### `(*HotKeyTracker) Top(n int) []KeyCount`

Up to `n` tracked keys, hottest first (ties broken by key for determinism).
`n <= 0` returns all tracked keys.

```go
for _, kc := range hk.Top(10) {
	log.Printf("%s ~%d reads", kc.Key, kc.Est)
}
```

### `KeyCount`

`type KeyCount struct { Key string; Est int64 }` — one `Top` entry; `Est` is
the estimated access count (an upper bound).

### `(*HotKeyTracker) Reset()`

Clears all tracking — call per sampling window.

```go
ticker := time.NewTicker(time.Minute)
for range ticker.C {
	report(hk.Top(20))
	hk.Reset() // fresh window
}
```

---

## Per-namespace stats — `NamespaceStats`

A great global hit-rate can hide one broken feature. `NamespaceStats` records
hit/miss/set/delete per namespace.

### `NamespaceFn`

`type NamespaceFn func(key string) string` — derives a metrics bucket from a
key.

### `DefaultNamespaceFn(key string) string`

Buckets by the prefix before the first `":"` (so `user:42` and `user:99` →
`user`; keys without `:` → `""`, the overall bucket).

```go
_ = cache.DefaultNamespaceFn("user:42") // "user"
```

### `NewNamespaceStats(c Cache, nsFn NamespaceFn) *NamespaceStats`

One-line: wraps `c`, bucketing each op (one map lookup) into per-namespace
counters. It is itself a `Cache`. `nsFn` may be nil (uses
`DefaultNamespaceFn`).

Use cases: alert when *one* feature's hit-rate collapses; dashboard a hit-rate
breakdown per entity type; supply a custom `nsFn` for non-colon key schemes.

```go
ns := cache.NewNamespaceStats(mem.New(), nil)
c := ns // use everywhere
_ = c
```

### `(*NamespaceStats) ByNamespace() map[string]Stats`

Snapshot copy of per-namespace counters.

```go
for name, s := range ns.ByNamespace() {
	log.Printf("ns=%q hit-rate=%.2f sets=%d", name, s.HitRatio(), s.Sets)
}
```
