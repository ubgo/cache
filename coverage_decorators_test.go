// coverage_decorators_test.go — branch coverage for the Cache decorators:
// Touch on every wrapper, retry ctx-cancel mid-backoff, breaker half-open
// re-open, bulkhead saturation + ctx error, NewBulkhead/NewHotKeyTracker
// clamps, audit every mutating op, invalidation Publish ctx-cancel, and
// NamespaceStats GetMulti error path. Test-only.

package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

// exerciseMutations drives every mutating op so decorator overrides
// (especially Touch) are covered.
func exerciseMutations(ctx context.Context, t *testing.T, c cache.Cache) {
	t.Helper()
	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMulti(ctx, map[string]cache.Item{"m": {Value: []byte("x"), TTL: time.Minute}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetNX(ctx, "nx", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.Expire(ctx, "k", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Touch(ctx, "k"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if _, err := c.Incr(ctx, "ctr", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decr(ctx, "ctr", 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteByPrefix(ctx, "m"); err != nil {
		t.Fatal(err)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAuditLogEveryMutatingOp(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	ops := map[string]int{}
	c := cache.NewAuditLog(cachetest.NewMock(), func(ev cache.AuditEvent) {
		mu.Lock()
		ops[ev.Op]++
		if ev.At.IsZero() {
			t.Error("audit event missing timestamp")
		}
		mu.Unlock()
	})
	exerciseMutations(ctx, t, c)
	for _, want := range []string{"set", "setmulti", "setnx", "expire", "touch", "incr", "decr", "del", "deletebyprefix", "flush"} {
		if ops[want] == 0 {
			t.Errorf("audit did not emit %q", want)
		}
	}

	// nil AuditFunc is a safe no-op.
	c2 := cache.NewAuditLog(cachetest.NewMock(), nil)
	if err := c2.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestBulkheadTouchAndClampAndCtx(t *testing.T) {
	ctx := context.Background()
	// maxConcurrent < 1 clamps to 1.
	c := cache.NewBulkhead(cachetest.NewMock(), 0)
	exerciseMutations(ctx, t, c)

	// Saturate the single slot, then a ctx-cancelled call returns ctx.Err().
	block := make(chan struct{})
	gate := &gatedMock{Mock: cachetest.NewMock(), release: block}
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
	if _, err := b.Get(cctx, "k"); err == nil {
		t.Fatal("saturated bulkhead with cancelled ctx must error")
	}
	close(block)
}

// gatedMock blocks the first Get until release is closed.
type gatedMock struct {
	*cachetest.Mock
	release chan struct{}
	once    sync.Once
	inside  chan struct{}
}

func (g *gatedMock) waitInside() {
	g.once.Do(func() { g.inside = make(chan struct{}) })
	<-g.inside
}

func (g *gatedMock) Get(ctx context.Context, key string) ([]byte, error) {
	g.once.Do(func() { g.inside = make(chan struct{}) })
	if key == "slow" {
		close(g.inside)
		<-g.release
	}
	return g.Mock.Get(ctx, key)
}

func TestBreakerTouchAndHalfOpenReopen(t *testing.T) {
	ctx := context.Background()
	base := cachetest.NewMock()
	fk := &flaky{Mock: base, failGets: 100}
	cb := cache.NewCircuitBreaker(fk,
		cache.WithBreakerThreshold(2),
		cache.WithBreakerCooldown(40*time.Millisecond))

	// Trip it.
	for i := 0; i < 2; i++ {
		_, _ = cb.Get(ctx, "k")
	}
	if _, err := cb.Get(ctx, "k"); !errors.Is(err, cache.ErrCircuitOpen) {
		t.Fatalf("want open, got %v", err)
	}
	// Cooldown elapses -> half-open trial; backend still failing -> re-open.
	time.Sleep(60 * time.Millisecond)
	if _, err := cb.Get(ctx, "k"); !errors.Is(err, errBackend) {
		t.Fatalf("half-open trial should hit backend, got %v", err)
	}
	if _, err := cb.Get(ctx, "k"); !errors.Is(err, cache.ErrCircuitOpen) {
		t.Fatalf("failed trial must re-open, got %v", err)
	}

	// Touch passes through breaker when closed.
	cb2 := cache.NewCircuitBreaker(cachetest.NewMock())
	exerciseMutations(ctx, t, cb2)
}

func TestRetryTouchAndCtxCancelMidBackoff(t *testing.T) {
	ctx := context.Background()

	// Touch passes through retrier.
	exerciseMutations(ctx, t, cache.NewRetry(cachetest.NewMock(), cache.WithRetryBackoff(time.Microsecond)))

	// NewRetry attempts<1 clamps to 1.
	r1 := cache.NewRetry(cachetest.NewMock(), cache.WithRetryAttempts(0))
	if _, err := r1.Get(ctx, "absent"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("clamped retrier: %v", err)
	}

	// ctx cancelled before the backoff sleep -> retryGuard returns ctx.Err().
	fk := &flaky{Mock: cachetest.NewMock(), failGets: 99}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := cache.NewRetry(fk, cache.WithRetryAttempts(5), cache.WithRetryBackoff(time.Hour))
	if _, err := r.Get(cctx, "k"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled aborting backoff, got %v", err)
	}
	if fk.getCalls != 1 {
		t.Fatalf("cancelled retry should attempt once, got %d", fk.getCalls)
	}
}

func TestHotKeyTrackerClampAndTopReset(t *testing.T) {
	ctx := context.Background()
	// capacity < 1 clamps to 1.
	hk := cache.NewHotKeyTracker(cachetest.NewMock(), 0)
	for i := 0; i < 3; i++ {
		_, _ = hk.Get(ctx, "a")
	}
	_, _ = hk.Get(ctx, "b") // evicts min, inherits +1
	_, _ = hk.GetMulti(ctx, []string{"b", "c"})
	if _, err := hk.TTL(ctx, "a"); err == nil {
		// TTL passes through; absent key -> ErrNotFound expected, fine either way.
		_ = err
	}
	top := hk.Top(1)
	if len(top) != 1 {
		t.Fatalf("Top(1) with cap 1 -> %d entries", len(top))
	}
	allTop := hk.Top(0) // n<=0 returns all
	if len(allTop) != 1 {
		t.Fatalf("Top(0) -> %d", len(allTop))
	}
	hk.Reset()
	if len(hk.Top(10)) != 0 {
		t.Fatal("Reset did not clear")
	}
}

func TestInvalidationPublishCtxCancel(t *testing.T) {
	bus := cache.NewInProcInvalidation()
	ctx, cancel := context.WithCancel(context.Background())

	// Subscriber that never drains -> Publish blocks then ctx cancels.
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = bus.Subscribe(subCtx, func(string) {
			<-subCtx.Done() // block delivery so the channel buffer fills
		})
	}()
	// Give Subscribe a moment to register.
	time.Sleep(20 * time.Millisecond)

	cancel() // cancel publish ctx
	// Publishing more than the 256 buffer with a stuck subscriber + cancelled
	// ctx must return ctx.Err() rather than block forever.
	keys := make([]string, 512)
	for i := range keys {
		keys[i] = "k"
	}
	if err := bus.Publish(ctx, keys...); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish with cancelled ctx should return context.Canceled, got %v", err)
	}
	subCancel()
	wg.Wait()

	// InvalidateAll sentinel delivered to a live subscriber.
	bus2 := cache.NewInProcInvalidation()
	sctx, scancel := context.WithCancel(context.Background())
	got := make(chan string, 1)
	go func() { _ = bus2.Subscribe(sctx, func(k string) { got <- k }) }()
	time.Sleep(20 * time.Millisecond)
	if err := bus2.Publish(context.Background(), cache.InvalidateAll); err != nil {
		t.Fatal(err)
	}
	select {
	case k := <-got:
		if k != cache.InvalidateAll {
			t.Fatalf("want InvalidateAll sentinel, got %q", k)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive InvalidateAll")
	}
	scancel()
}

func TestNamespaceStatsGetMultiError(t *testing.T) {
	ctx := context.Background()
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"getmulti": errors.New("gm")}
	ns := cache.NewNamespaceStats(fc, nil)
	if _, err := ns.GetMulti(ctx, []string{"user:1"}); err == nil {
		t.Fatal("NamespaceStats.GetMulti should surface backend error")
	}
	// Set error path: counter not incremented when backend fails.
	sf := cachetest.NewMock()
	sf.FailOn = map[string]error{"set": errors.New("s")}
	ns2 := cache.NewNamespaceStats(sf, nil)
	if err := ns2.Set(ctx, "user:1", []byte("v"), time.Minute); err == nil {
		t.Fatal("expected set error")
	}
	if s := ns2.ByNamespace(); s["user"].Sets != 0 {
		t.Fatal("failed Set must not count")
	}
	// Del error path.
	df := cachetest.NewMock()
	df.FailOn = map[string]error{"del": errors.New("d")}
	ns3 := cache.NewNamespaceStats(df, nil)
	if err := ns3.Del(ctx, "user:1"); err == nil {
		t.Fatal("expected del error")
	}
}
