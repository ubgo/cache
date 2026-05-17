// otel_test.go — tests for the OpenTelemetry ObsHooks exporter (contrib/cache-otel/otel.go).

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

func TestOtelHooksRecordOps(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	hooks, err := cacheotel.New(mp.Meter("cache"), "mock", "test")
	if err != nil {
		t.Fatal(err)
	}
	c := cache.Instrument(cachetest.NewMock(), hooks)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	_, _ = c.Get(ctx, "k")    // hit
	_, _ = c.Get(ctx, "nope") // miss

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}

	var opsTotal int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "cache.ops" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("cache.ops wrong type %T", m.Data)
			}
			for _, dp := range sum.DataPoints {
				opsTotal += dp.Value
			}
		}
	}
	if opsTotal != 3 { // set + hit + miss
		t.Fatalf("want 3 recorded ops, got %d", opsTotal)
	}
}
