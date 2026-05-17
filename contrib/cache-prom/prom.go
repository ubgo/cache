// prom.go — Prometheus exporter for cache.ObsHooks (package cacheprom, github.com/ubgo/cache/contrib/cache-prom).
//
// Package role: a standalone contrib MODULE (its own go.mod) so the
// Prometheus client never enters the dependency-free core; the next comment
// block is its canonical package doc (blank-line-separated so this header
// is not a duplicate package comment).
//
// This file: the entire module — New(reg, adapter, namespace) registers a
// counter + histogram and returns a cache.ObsHooks wired to them, consumed
// via cache.Instrument(backend, hooks). The WHY: ship Prometheus support
// without the core importing prometheus. Invariant: adapter/namespace
// become CONST labels so several caches can share one registry; New errors
// on registration failure (e.g. duplicate) rather than panicking.
//
// AI-context: this implements the producer side of obs.go's ObsHooks
// contract — field names/labels here must track OpEvent in the core.

// Package cacheprom adapts cache.ObsHooks to Prometheus. It keeps the core
// cache module dependency-free: the Prometheus client lives only here.
//
//	hooks, err := cacheprom.New(prometheus.DefaultRegisterer, "redis", "billing")
//	if err != nil { ... }
//	c := cache.Instrument(backend, hooks)
//
// Metrics registered:
//
//	cache_ops_total{op,result,adapter,namespace}        counter
//	cache_op_duration_seconds{op,adapter,namespace}     histogram
package cacheprom

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/ubgo/cache"
)

// New registers the collectors on reg and returns ObsHooks wired to them.
// adapter/namespace become constant labels so several caches can share one
// registry. Returns an error if registration fails (e.g. duplicate).
func New(reg prometheus.Registerer, adapter, namespace string) (cache.ObsHooks, error) {
	labels := prometheus.Labels{"adapter": adapter, "namespace": namespace}

	ops := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cache_ops_total",
		Help:        "Total cache operations by op and result.",
		ConstLabels: labels,
	}, []string{"op", "result"})

	dur := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "cache_op_duration_seconds",
		Help:        "Cache operation latency in seconds.",
		Buckets:     prometheus.DefBuckets,
		ConstLabels: labels,
	}, []string{"op"})

	// Register both collectors. A failure here is almost always a duplicate
	// registration (New called twice with the same const labels on the same
	// registry, or another collector owning the metric name). The error is
	// returned, never panicked, so callers can recover with a fresh registry
	// or distinct adapter/namespace labels.
	for _, c := range []prometheus.Collector{ops, dur} {
		if err := reg.Register(c); err != nil {
			return cache.ObsHooks{}, err
		}
	}

	return cache.ObsHooks{
		Adapter:   adapter,
		Namespace: namespace,
		OnOp: func(ev cache.OpEvent) {
			// Classification order is significant: ErrNotFound MUST be
			// tested before the generic Err!=nil branch, otherwise every
			// normal cache miss would be counted as result="error" and
			// pollute error-rate alerts. The third branch handles paths
			// that signal a miss via Hit=false with a nil error.
			result := "ok"
			switch {
			case errors.Is(ev.Err, cache.ErrNotFound):
				result = "miss" // a miss is expected, not an error
			case ev.Err != nil:
				result = "error"
			case ev.Op == "get" && !ev.Hit:
				result = "miss"
			}
			ops.WithLabelValues(ev.Op, result).Inc()
			// Latency is recorded for every op regardless of result; keeping
			// result off the histogram bounds series cardinality. Seconds
			// matches the _seconds metric suffix.
			dur.WithLabelValues(ev.Op).Observe(ev.Duration.Seconds())
		},
	}, nil
}
