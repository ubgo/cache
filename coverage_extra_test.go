// coverage_extra_test.go — residual branch coverage: WithCodec(nil) re-default,
// applyJitter negative-clamp, Once cached-result decode-failure fall-through,
// bulkheadDo ctx-error on a mutating op, and Warm ctx-cancel while blocked on
// a concurrency slot. Test-only.

package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestWithCodecNilRedefaults(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	// WithCodec(nil) must fall back to DefaultCodec in newRememberCfg.
	if err := cache.SetT(ctx, c, "k", 7, time.Minute, cache.WithCodec(nil)); err != nil {
		t.Fatal(err)
	}
	v, err := cache.GetT[int](ctx, c, "k", cache.WithCodec(nil))
	if err != nil || v != 7 {
		t.Fatalf("nil codec re-default roundtrip: %d %v", v, err)
	}
}

func TestJitterNegativeClampStaysPositive(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	// A jitter fraction > 1 can produce a negative multiplier; applyJitter must
	// clamp back to the original ttl rather than store a negative duration.
	for i := 0; i < 200; i++ {
		if err := cache.SetT(ctx, c, "k", 1, time.Hour, cache.WithJitter(3.0)); err != nil {
			t.Fatal(err)
		}
		d, err := c.TTL(ctx, "k")
		if err != nil || d <= 0 {
			t.Fatalf("jitter must never store a non-positive TTL, got %v %v", d, err)
		}
	}
}

func TestOnceCachedResultDecodeFailureFallsThrough(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	// Pre-store undecodable bytes at the result key. Once's initial Get finds
	// it but Decode fails -> it must fall through to take the lease and run fn.
	_ = c.Set(ctx, "idemp:res:k", []byte("not-json-int"), time.Minute)
	var ran int
	v, err := cache.Once(ctx, c, "k", time.Minute, func(context.Context) (int, error) {
		ran++
		return 42, nil
	})
	if err != nil || v != 42 || ran != 1 {
		t.Fatalf("decode-failure fall-through: v=%d err=%v ran=%d", v, err, ran)
	}

	// Lease held by another and cached result undecodable -> ErrInFlight.
	c2 := cachetest.NewMock()
	_, _ = c2.SetNX(ctx, "idemp:lock:x", []byte{1}, time.Minute)
	_ = c2.Set(ctx, "idemp:res:x", []byte("bad"), time.Minute)
	if _, err := cache.Once(ctx, c2, "x", time.Minute,
		func(context.Context) (int, error) { return 1, nil }); err != cache.ErrInFlight {
		t.Fatalf("want ErrInFlight when cached result undecodable, got %v", err)
	}
}

func TestBulkheadDoCtxErrorOnMutatingOp(t *testing.T) {
	gate := &gatedMock{Mock: cachetest.NewMock(), release: make(chan struct{})}
	b := cache.NewBulkhead(gate, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = b.Get(context.Background(), "slow")
	}()
	<-started
	gate.waitInside()
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Set goes through bulkheadDo; slot is saturated and ctx is cancelled.
	if err := b.Set(cctx, "k", []byte("v"), time.Minute); err == nil {
		t.Fatal("saturated bulkheadDo with cancelled ctx must error")
	}
	close(gate.release)
}

func TestWarmCancelWhileBlockedOnSlot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := cachetest.NewMock()
	release := make(chan struct{})
	first := make(chan struct{})
	var once sync.Once

	keys := []string{"a", "b", "c", "d"}
	done := make(chan []cache.WarmResult, 1)
	go func() {
		done <- cache.Warm(ctx, c, keys, time.Minute, 1,
			func(ctx context.Context, _ string) (int, error) {
				once.Do(func() { close(first) })
				<-release // hold the single slot
				return 1, nil
			})
	}()

	<-first  // first key's loader is running, holding the only slot
	cancel() // cancel while scheduler blocks on the sem-send select for key "b"
	close(release)

	res := <-done
	if len(res) != len(keys) {
		t.Fatalf("expected %d results, got %d", len(keys), len(res))
	}
	// At least one of the later keys must be marked with the cancellation
	// error (either via the priority ctx.Err() check or the select branch).
	cancelled := 0
	for _, r := range res {
		if r.Err == context.Canceled {
			cancelled++
		}
	}
	if cancelled == 0 {
		t.Fatalf("expected some keys marked cancelled, results: %+v", res)
	}
}
