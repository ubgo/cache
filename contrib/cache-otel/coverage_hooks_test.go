// coverage_hooks_test.go — exercises every OnOp result-classification branch
// by invoking the returned hooks directly (a custom-instrumentation caller
// can legitimately produce a nil-error get with Hit=false, which
// cache.Instrument itself never emits).

package cacheotel_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ubgo/cache"
	cacheotel "github.com/ubgo/cache/contrib/cache-otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestOnOpClassificationBranches(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	hooks, err := cacheotel.New(mp.Meter("cov"), "mock", "cov")
	if err != nil {
		t.Fatal(err)
	}
	if hooks.OnOp == nil {
		t.Fatal("OnOp must be set")
	}
	hooks.OnOp(cache.OpEvent{Op: "set", Hit: false, Err: nil, Duration: time.Millisecond})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: true, Err: nil, Duration: time.Millisecond})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: cache.ErrNotFound})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: errors.New("backend down")})
	hooks.OnOp(cache.OpEvent{Op: "get", Hit: false, Err: nil}) // nil-err miss branch
}
