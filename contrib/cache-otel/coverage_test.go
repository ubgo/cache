// coverage_test.go — fills remaining branches in New: the generic error
// classification, duration histogram recording + attribute set, and the two
// instrument-creation error returns (via a meter that fails Int64Counter /
// Float64Histogram). Deterministic: manual reader + a tiny erroring meter
// built on the otel embedded interface (no production seam added).

package cacheotel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheotel "github.com/ubgo/cache/contrib/cache-otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// errMeter embeds a real metric.Meter (so it inherits every method and stays
// forward-compatible with interface additions) and overrides only the two
// constructors New calls, letting each be forced to fail. This reaches both
// error returns in New deterministically without touching production code.
type errMeter struct {
	metric.Meter  // embedded noop meter supplies all other methods
	failCounter   bool
	failHistogram bool
}

var errBoom = errors.New("instrument creation failed")

func (m errMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.failCounter {
		return nil, errBoom
	}
	return m.Meter.Int64Counter(name, opts...)
}

func (m errMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if m.failHistogram {
		return nil, errBoom
	}
	return m.Meter.Float64Histogram(name, opts...)
}

func TestNewCounterCreationError(t *testing.T) {
	m := errMeter{Meter: noop.NewMeterProvider().Meter("x"), failCounter: true}
	if _, err := cacheotel.New(m, "a", "b"); !errors.Is(err, errBoom) {
		t.Fatalf("expected counter creation error, got %v", err)
	}
}

func TestNewHistogramCreationError(t *testing.T) {
	m := errMeter{Meter: noop.NewMeterProvider().Meter("x"), failHistogram: true}
	if _, err := cacheotel.New(m, "a", "b"); !errors.Is(err, errBoom) {
		t.Fatalf("expected histogram creation error, got %v", err)
	}
}

// TestOtelErrorClassificationAndDuration drives the generic ev.Err != nil
// branch and asserts the duration histogram + attribute set are emitted.
func TestOtelErrorClassificationAndDuration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	hooks, err := cacheotel.New(mp.Meter("cache"), "mock", "errs")
	if err != nil {
		t.Fatal(err)
	}
	mock := cachetest.NewMock()
	mock.FailOn = map[string]error{"set": errors.New("backend down")}
	c := cache.Instrument(mock, hooks)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), time.Minute) // generic error path
	_, _ = c.Get(ctx, "miss")                     // ErrNotFound -> miss

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}

	var sawErrorResult, sawDuration bool
	for _, sm := range rm.ScopeMetrics {
		for _, mt := range sm.Metrics {
			switch mt.Name {
			case "cache.ops":
				sum, ok := mt.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("cache.ops wrong type %T", mt.Data)
				}
				for _, dp := range sum.DataPoints {
					if v, ok := dp.Attributes.Value("result"); ok && v.AsString() == "error" {
						sawErrorResult = true
					}
					// attribute set must carry adapter/namespace
					if v, ok := dp.Attributes.Value("adapter"); !ok || v.AsString() != "mock" {
						t.Fatalf("missing/wrong adapter attr: %v", dp.Attributes)
					}
				}
			case "cache.op.duration":
				h, ok := mt.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("duration wrong type %T", mt.Data)
				}
				if len(h.DataPoints) > 0 {
					sawDuration = true
				}
			}
		}
	}
	if !sawErrorResult {
		t.Fatal("generic error op should classify as result=error")
	}
	if !sawDuration {
		t.Fatal("duration histogram should record at least one point")
	}
}
