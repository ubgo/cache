package cacheprom_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheprom "github.com/ubgo/cache/contrib/cache-prom"
)

func TestPromHooksRecordOps(t *testing.T) {
	reg := prometheus.NewRegistry()
	hooks, err := cacheprom.New(reg, "mock", "test")
	if err != nil {
		t.Fatal(err)
	}
	c := cache.Instrument(cachetest.NewMock(), hooks)
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	_, _ = c.Get(ctx, "k")    // hit
	_, _ = c.Get(ctx, "nope") // miss

	got := testutil.CollectAndCount(reg, "cache_ops_total")
	if got == 0 {
		t.Fatal("no cache_ops_total samples recorded")
	}

	expected := `
# HELP cache_ops_total Total cache operations by op and result.
# TYPE cache_ops_total counter
cache_ops_total{adapter="mock",namespace="test",op="get",result="miss"} 1
cache_ops_total{adapter="mock",namespace="test",op="get",result="ok"} 1
cache_ops_total{adapter="mock",namespace="test",op="set",result="ok"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "cache_ops_total"); err != nil {
		t.Fatalf("metrics mismatch: %v", err)
	}
}

func TestDuplicateRegistrationErrors(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := cacheprom.New(reg, "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheprom.New(reg, "a", "b"); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
