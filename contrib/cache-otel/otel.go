// otel.go — OpenTelemetry-metrics exporter for cache.ObsHooks (package cacheotel, github.com/ubgo/cache/contrib/cache-otel).
//
// Package role: a standalone contrib MODULE (its own go.mod) so the otel
// SDK never enters the dependency-free core; the next comment block is its
// canonical package doc (blank-line-separated so this header is not a
// duplicate package comment).
//
// This file: the entire module — New(meter, adapter, namespace) builds an
// Int64Counter + Float64Histogram and returns a cache.ObsHooks wired to
// them, consumed via cache.Instrument(backend, hooks). The WHY: ship otel
// support without the core importing the otel SDK. Invariant: adapter/
// namespace are emitted as attributes; New returns an error if instrument
// creation fails rather than panicking.
//
// AI-context: producer side of obs.go's ObsHooks contract — instrument
// names/attributes here must track OpEvent in the core (sibling of the
// cache-prom exporter, same contract, different backend).

// Package cacheotel adapts cache.ObsHooks to OpenTelemetry metrics. It keeps
// the core cache module dependency-free: the otel SDK lives only here.
//
//	hooks, err := cacheotel.New(otel.Meter("cache"), "redis", "billing")
//	if err != nil { ... }
//	c := cache.Instrument(backend, hooks)
//
// Instruments:
//
//	cache.ops        Int64Counter      (attrs: op, result, adapter, namespace)
//	cache.op.duration Float64Histogram (attrs: op, adapter, namespace; seconds)
package cacheotel

import (
	"context"
	"errors"

	"github.com/ubgo/cache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// New builds the instruments on meter and returns ObsHooks wired to them.
func New(meter metric.Meter, adapter, namespace string) (cache.ObsHooks, error) {
	ops, err := meter.Int64Counter("cache.ops",
		metric.WithDescription("Total cache operations by op and result."))
	if err != nil {
		return cache.ObsHooks{}, err
	}
	dur, err := meter.Float64Histogram("cache.op.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Cache operation latency in seconds."))
	if err != nil {
		return cache.ObsHooks{}, err
	}

	// base is captured by the OnOp closure and shared across every (possibly
	// concurrent) operation. It is never mutated; per-call attribute slices
	// are built by copying it into a fresh slice — see OnOp below.
	base := []attribute.KeyValue{
		attribute.String("adapter", adapter),
		attribute.String("namespace", namespace),
	}

	return cache.ObsHooks{
		Adapter:   adapter,
		Namespace: namespace,
		OnOp: func(ev cache.OpEvent) {
			// Classification order is significant: ErrNotFound MUST precede
			// the generic Err!=nil branch, else normal misses would be
			// recorded as result="error" and skew SLOs. The third branch
			// covers nil-error misses signalled via Hit=false.
			result := "ok"
			switch {
			case errors.Is(ev.Err, cache.ErrNotFound):
				result = "miss"
			case ev.Err != nil:
				result = "error"
			case ev.Op == "get" && !ev.Hit:
				result = "miss"
			}
			// append([]attribute.KeyValue{}, base...) starts from a fresh
			// backing array on purpose: appending directly to the shared
			// base slice could clobber concurrent callers if base had spare
			// capacity. Do not collapse into append(base, ...).
			attrs := append(append([]attribute.KeyValue{}, base...),
				attribute.String("op", ev.Op),
				attribute.String("result", result),
			)
			// No caller ctx is available in the hook; Background is fine —
			// these are fire-and-forget metric writes.
			ctx := context.Background()
			ops.Add(ctx, 1, metric.WithAttributes(attrs...))
			// Histogram intentionally omits the result attribute to bound
			// time-series cardinality; latency matters for every outcome.
			// Same fresh-slice copy reasoning as above.
			dur.Record(ctx, ev.Duration.Seconds(), metric.WithAttributes(
				append(append([]attribute.KeyValue{}, base...),
					attribute.String("op", ev.Op))...))
		},
	}, nil
}
