// coverage_test.go — fills the remaining branches in New: the generic
// error classification (ev.Err != nil, not ErrNotFound) and the second
// collector's registration-failure return (dur conflicts while ops is new).

package cacheprom_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	cacheprom "github.com/ubgo/cache/contrib/cache-prom"
)

// TestPromErrorClassification drives the ev.Err != nil branch (a non-
// ErrNotFound error must be counted result="error", never "miss").
func TestPromErrorClassification(t *testing.T) {
	reg := prometheus.NewRegistry()
	hooks, err := cacheprom.New(reg, "mock", "errs")
	if err != nil {
		t.Fatal(err)
	}
	m := cachetest.NewMock()
	boom := errors.New("backend exploded")
	m.FailOn = map[string]error{"set": boom}
	c := cache.Instrument(m, hooks)

	if e := c.Set(context.Background(), "k", []byte("v"), time.Minute); !errors.Is(e, boom) {
		t.Fatalf("expected injected error, got %v", e)
	}

	expected := `
# HELP cache_ops_total Total cache operations by op and result.
# TYPE cache_ops_total counter
cache_ops_total{adapter="mock",namespace="errs",op="set",result="error"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "cache_ops_total"); err != nil {
		t.Fatalf("error op should classify as result=error: %v", err)
	}
}

// TestPromMissViaErrNotFound exercises the ErrNotFound branch explicitly
// (must classify as "miss", not "error").
func TestPromMissViaErrNotFound(t *testing.T) {
	reg := prometheus.NewRegistry()
	hooks, err := cacheprom.New(reg, "mock", "miss")
	if err != nil {
		t.Fatal(err)
	}
	m := cachetest.NewMock()
	m.FailOn = map[string]error{"get": cache.ErrNotFound}
	c := cache.Instrument(m, hooks)

	if _, e := c.Get(context.Background(), "absent"); !errors.Is(e, cache.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", e)
	}
	expected := `
# HELP cache_ops_total Total cache operations by op and result.
# TYPE cache_ops_total counter
cache_ops_total{adapter="mock",namespace="miss",op="get",result="miss"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "cache_ops_total"); err != nil {
		t.Fatalf("ErrNotFound should classify as result=miss: %v", err)
	}
}

// TestSecondCollectorRegistrationFails pre-registers a collector owning the
// cache_op_duration_seconds name so ops registers fine but dur fails — the
// only way to hit the second loop iteration's error return.
func TestSecondCollectorRegistrationFails(t *testing.T) {
	reg := prometheus.NewRegistry()
	clash := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cache_op_duration_seconds",
		Help: "squatter",
	})
	if err := reg.Register(clash); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheprom.New(reg, "a", "b"); err == nil {
		t.Fatal("expected dur registration to fail")
	}
}
