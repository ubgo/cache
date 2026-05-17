# cache-otel — feature cookbook

Every exported identifier in `github.com/ubgo/cache/contrib/cache-otel`, with concrete use cases and runnable snippets. The exported surface is exactly one function: `New`. The instruments it creates are documented after it because they are the real end-user contract.

Import paths used throughout:

```go
import (
    "github.com/ubgo/cache"
    cacheotel "github.com/ubgo/cache/contrib/cache-otel"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)
```

---

### New

```go
func New(meter metric.Meter, adapter, namespace string) (cache.ObsHooks, error)
```

Builds an `Int64Counter` named `cache.ops` and a `Float64Histogram` named `cache.op.duration` (unit `s`) on `meter`, and returns a `cache.ObsHooks` whose `OnOp` callback writes to them. `adapter` and `namespace` are attached as attributes to every recorded measurement. Returns an error (never panics) if instrument creation fails.

**Use cases:**

- Emit cache metrics through your existing OTel pipeline to an OTLP collector / Prometheus / vendor backend, without the core importing the OTel SDK.
- Use a manual reader in tests to assert exact counter/histogram values deterministically.
- Label several caches differently (`adapter`, `namespace`) while sharing one `MeterProvider`.
- Correlate cache latency with traces by exporting via the same OTel SDK that produces your spans.

Wiring with the global meter:

```go
package main

import (
	"context"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheotel "github.com/ubgo/cache/contrib/cache-otel"
	"go.opentelemetry.io/otel"
)

func main() {
	backend := cachetest.NewMock()

	hooks, err := cacheotel.New(otel.Meter("cache"), "memory", "billing")
	if err != nil {
		panic(err)
	}

	c := cache.Instrument(backend, hooks)

	ctx := context.Background()
	_, _ = cache.Remember(ctx, c, "user:42", time.Minute,
		func(ctx context.Context) (string, error) { return "Ada", nil })
	_, _ = cache.Remember(ctx, c, "user:42", time.Minute,
		func(ctx context.Context) (string, error) { return "Ada", nil }) // hit
}
```

Manual-reader usage (deterministic in tests — collect and inspect without an exporter):

```go
package cacheotel_test

import (
	"context"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheotel "github.com/ubgo/cache/contrib/cache-otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestCacheMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	hooks, err := cacheotel.New(mp.Meter("cache"), "memory", "test")
	if err != nil {
		t.Fatal(err)
	}
	c := cache.Instrument(cachetest.NewMock(), hooks)

	ctx := context.Background()
	_, _ = cache.Remember(ctx, c, "k", time.Minute,
		func(ctx context.Context) (int, error) { return 1, nil })

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	// rm.ScopeMetrics now contains cache.ops and cache.op.duration with
	// attributes adapter="memory", namespace="test", op, result.
	_ = rm
}
```

---

### Created instruments

Not Go-level exports, but the actual end-user observable contract produced by `New`.

#### cache.ops

`Int64Counter`, description "Total cache operations by op and result." Incremented by 1 per operation, with attributes `op`, `result`, `adapter`, `namespace`.

**Use cases:**

- Hit rate per namespace from the counter stream.
- Error-rate alerting on `result="error"`.
- Per-`op` traffic breakdown.

#### cache.op.duration

`Float64Histogram`, unit `s`, description "Cache operation latency in seconds." Recorded for **every** operation with attributes `op`, `adapter`, `namespace`. The `result` attribute is intentionally **omitted** to bound cardinality.

**Use cases:**

- Latency distribution / percentiles per `op` and `namespace`.
- Detect a slow backend independent of hit/miss/error accounting.

#### Attributes and result classification

`adapter` and `namespace` come from the `New` arguments and are attached to every measurement. `op` is the operation name. The `result` attribute (counter only) is classified, **order significant**:

| Condition (checked in order) | `result` |
|---|---|
| `errors.Is(ev.Err, cache.ErrNotFound)` | `miss` |
| `ev.Err != nil` | `error` |
| `ev.Op == "get" && !ev.Hit` | `miss` |
| otherwise | `ok` |

`ErrNotFound` is tested **before** the generic `Err != nil` branch so normal misses are not recorded as `result="error"` and do not skew SLOs. The third branch covers nil-error misses signalled via `Hit=false`.

Concurrency note: the per-call attribute slice is built by copying the shared `base` (`adapter`/`namespace`) into a **fresh** backing array before appending `op`/`result`. This is deliberate — appending directly to the shared slice could clobber concurrent callers. The hook uses `context.Background()` because no caller context is available; metric writes are fire-and-forget.
