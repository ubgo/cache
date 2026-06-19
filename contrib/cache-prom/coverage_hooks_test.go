// coverage_hooks_test.go — exercises every OnOp result-classification branch
// by invoking the returned hooks directly (a custom-instrumentation caller
// can legitimately produce a nil-error get with Hit=false, which
// cache.Instrument itself never emits).

package cacheprom_test

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/ubgo/cache"
	cacheprom "github.com/ubgo/cache/contrib/cache-prom"
)

func TestOnOpClassificationBranches(t *testing.T) {
	reg := prometheus.NewRegistry()
	hooks, err := cacheprom.New(reg, "mock", "cov")
	if err != nil {
		t.Fatal(err)
	}
	if hooks.OnOp == nil {
		t.Fatal("OnOp must be set")
	}
	// ok, miss-via-ErrNotFound, error-via-other-err, and the nil-error
	// get-with-Hit=false miss branch.
	hooks.OnOp(cache.OpEvent{Op: "set", Hit: false, Err: nil, Duration: time.Millisecond})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: true, Err: nil, Duration: time.Millisecond})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: cache.ErrNotFound})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: errors.New("backend down")})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: nil}) // nil-err miss branch
}
