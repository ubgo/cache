# cache-prom — feature cookbook

Every exported identifier in `github.com/ubgo/cache/contrib/cache-prom`, with concrete use cases and runnable snippets. The exported surface is exactly one function: `New`. The metrics it registers are documented after it because, although not Go exports, they are the actual end-user contract.

Import paths used throughout:

```go
import (
    "github.com/ubgo/cache"
    cacheprom "github.com/ubgo/cache/contrib/cache-prom"
)
```

---

### New

```go
func New(reg prometheus.Registerer, adapter, namespace string) (cache.ObsHooks, error)
```

Registers two collectors (`cache_ops_total` counter, `cache_op_duration_seconds` histogram) on `reg`, and returns a `cache.ObsHooks` whose `OnOp` callback updates them. `adapter` and `namespace` become **constant labels** on both metrics, so several caches can register on the same registry without colliding. Returns an error (never panics) if registration fails — almost always a duplicate registration.

**Use cases:**

- Add Prometheus visibility to any `ubgo/cache` backend (Redis, in-memory, tiered) without the core importing the Prometheus client.
- Run several instrumented caches in one process — one Redis cache labelled `adapter="redis",namespace="billing"`, one in-memory cache labelled `adapter="memory",namespace="sessions"` — all on `prometheus.DefaultRegisterer`.
- Expose cache hit-rate and latency on your existing `/metrics` endpoint for Grafana dashboards and alerting.
- Detect a misconfigured double-instrumentation in tests by asserting the returned error on a duplicate `New` call.

Basic wiring onto the default registry and the standard `/metrics` handler:

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheprom "github.com/ubgo/cache/contrib/cache-prom"
)

func main() {
	// Any cache.Cache works; cachetest.NewMock is a correct in-memory backend.
	backend := cachetest.NewMock()

	hooks, err := cacheprom.New(prometheus.DefaultRegisterer, "memory", "billing")
	if err != nil {
		panic(err)
	}

	c := cache.Instrument(backend, hooks)

	// Drive some traffic.
	ctx := context.Background()
	_, _ = cache.Remember(ctx, c, "user:42", time.Minute,
		func(ctx context.Context) (string, error) { return "Ada", nil })
	_, _ = cache.Remember(ctx, c, "user:42", time.Minute,
		func(ctx context.Context) (string, error) { return "Ada", nil }) // hit

	http.Handle("/metrics", promhttp.Handler())
	_ = http.ListenAndServe(":2112", nil)
}
```

Several caches sharing one registry via distinct const labels:

```go
reg := prometheus.NewRegistry()

billingHooks, err := cacheprom.New(reg, "redis", "billing")
if err != nil {
	panic(err)
}
sessionHooks, err := cacheprom.New(reg, "memory", "sessions")
if err != nil {
	panic(err)
}

billing := cache.Instrument(cachetest.NewMock(), billingHooks)
sessions := cache.Instrument(cachetest.NewMock(), sessionHooks)
_, _ = billing, sessions
// cache_ops_total now carries adapter/namespace const labels distinguishing the two.
```

Duplicate-registration error (calling `New` twice with the **same** const labels on the **same** registry):

```go
reg := prometheus.NewRegistry()

if _, err := cacheprom.New(reg, "redis", "billing"); err != nil {
	panic(err) // first call: succeeds
}

_, err := cacheprom.New(reg, "redis", "billing")
if err != nil {
	// Expected: prometheus.AlreadyRegisteredError — recover by using a
	// fresh registry or distinct adapter/namespace labels. New never panics.
	log.Printf("duplicate registration: %v", err)
}
```

---

### Registered metrics

Not Go-level exports, but the actual end-user observable contract produced by `New`. Both are registered on the `prometheus.Registerer` you pass in.

#### cache_ops_total

```
cache_ops_total{op, result, adapter, namespace}   counter
```

Total cache operations, incremented once per operation. `op` and `result` are variable labels; `adapter` and `namespace` are the constant labels from `New`.

**Use cases:**

- Compute hit rate: `rate(cache_ops_total{result="ok"}[5m]) / rate(cache_ops_total{op="get"}[5m])`.
- Alert on error rate: `rate(cache_ops_total{result="error"}[5m]) > 0`.
- Break traffic down per `op` (`get`, `set`, `delete`, …) and per `namespace`.

#### cache_op_duration_seconds

```
cache_op_duration_seconds{op, adapter, namespace}   histogram (prometheus.DefBuckets)
```

Cache operation latency in seconds, observed for **every** operation regardless of result. `result` is intentionally **omitted** from the histogram to bound time-series cardinality.

**Use cases:**

- p99 latency: `histogram_quantile(0.99, rate(cache_op_duration_seconds_bucket[5m]))`.
- Compare latency across `op` and `namespace`.
- Detect a slow backend (rising buckets) independent of error/miss accounting.

#### Result classification

The `OnOp` closure classifies every operation into a `result` label. **Order matters** and is enforced:

| Condition (checked in order) | `result` |
|---|---|
| `errors.Is(ev.Err, cache.ErrNotFound)` | `miss` |
| `ev.Err != nil` | `error` |
| `ev.Op == "get" && !ev.Hit` | `miss` |
| otherwise | `ok` |

`cache.ErrNotFound` is tested **before** the generic `Err != nil` branch on purpose: a normal cache miss must not be counted as `result="error"`, or every miss would pollute error-rate alerts. The third branch covers backends that signal a miss via `Hit=false` with a `nil` error.

```go
// Conceptually, for each completed operation:
result := "ok"
switch {
case errors.Is(ev.Err, cache.ErrNotFound):
	result = "miss" // expected, not an error
case ev.Err != nil:
	result = "error"
case ev.Op == "get" && !ev.Hit:
	result = "miss"
}
// cache_ops_total{op: ev.Op, result: result}.Inc()
// cache_op_duration_seconds{op: ev.Op}.Observe(ev.Duration.Seconds())
```
