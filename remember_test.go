package cache_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestRememberCachesAndReuses(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var calls int64
	load := func(ctx context.Context) (int, error) {
		atomic.AddInt64(&calls, 1)
		return 42, nil
	}
	for i := 0; i < 5; i++ {
		v, err := cache.Remember(ctx, c, "k", time.Minute, load)
		if err != nil || v != 42 {
			t.Fatalf("got %d, %v", v, err)
		}
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("loader called %d times, want 1", calls)
	}
}

func TestRememberNegativeCaching(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var calls int64
	load := func(ctx context.Context) (string, error) {
		atomic.AddInt64(&calls, 1)
		return "", cache.ErrNotFound
	}
	for i := 0; i < 3; i++ {
		_, err := cache.Remember(ctx, c, "missing", time.Minute, load,
			cache.WithNegativeTTL(time.Minute))
		if err != cache.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("negative cache failed: loader called %d times, want 1", calls)
	}
}

func TestRememberStaleIfError(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var fail atomic.Bool
	load := func(ctx context.Context) (string, error) {
		if fail.Load() {
			return "", context.DeadlineExceeded
		}
		return "good", nil
	}
	// Prime with a very short soft TTL but a stale-if-error window.
	v, err := cache.Remember(ctx, c, "k", 40*time.Millisecond, load,
		cache.WithStaleIfError(time.Minute))
	if err != nil || v != "good" {
		t.Fatalf("prime: %q %v", v, err)
	}
	time.Sleep(80 * time.Millisecond) // soft-expire
	fail.Store(true)
	v, err = cache.Remember(ctx, c, "k", 40*time.Millisecond, load,
		cache.WithStaleIfError(time.Minute))
	if err != nil || v != "good" {
		t.Fatalf("stale-if-error should serve last good value, got %q %v", v, err)
	}
}

func TestRememberStaleWhileRevalidate(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var n int64
	load := func(ctx context.Context) (int64, error) {
		return atomic.AddInt64(&n, 1), nil
	}
	v, _ := cache.Remember(ctx, c, "k", 40*time.Millisecond, load,
		cache.WithStaleWhileRevalidate(time.Minute))
	if v != 1 {
		t.Fatalf("first load want 1, got %d", v)
	}
	time.Sleep(80 * time.Millisecond) // soft-expired, still in stale window
	v, _ = cache.Remember(ctx, c, "k", 40*time.Millisecond, load,
		cache.WithStaleWhileRevalidate(time.Minute))
	if v != 1 {
		t.Fatalf("SWR must return stale value immediately, got %d", v)
	}
	eventually(t, func() bool { return atomic.LoadInt64(&n) == 2 }) // bg refresh ran
}

func TestRememberRefreshAhead(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var n int64
	load := func(ctx context.Context) (int64, error) {
		return atomic.AddInt64(&n, 1), nil
	}
	_, _ = cache.Remember(ctx, c, "k", 100*time.Millisecond, load,
		cache.WithRefreshAhead(0.5))
	time.Sleep(70 * time.Millisecond) // past 50% of TTL, still fresh
	v, _ := cache.Remember(ctx, c, "k", 100*time.Millisecond, load,
		cache.WithRefreshAhead(0.5))
	if v != 1 {
		t.Fatalf("refresh-ahead must still return current value, got %d", v)
	}
	eventually(t, func() bool { return atomic.LoadInt64(&n) == 2 }) // refreshed in bg
}

func TestGetSetTypedRoundtrip(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	type user struct {
		ID   int
		Name string
	}
	in := user{ID: 7, Name: "ada"}
	if err := cache.SetT(ctx, c, "u", in, time.Minute); err != nil {
		t.Fatal(err)
	}
	out, err := cache.GetT[user](ctx, c, "u")
	if err != nil || out != in {
		t.Fatalf("roundtrip: %+v %v", out, err)
	}
}

func TestJitterStaysInBounds(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	for i := 0; i < 100; i++ {
		if err := cache.SetT(ctx, c, "k", 1, time.Hour, cache.WithJitter(0.1)); err != nil {
			t.Fatal(err)
		}
		d, err := c.TTL(ctx, "k")
		if err != nil {
			t.Fatal(err)
		}
		if d < 54*time.Minute || d > 66*time.Minute {
			t.Fatalf("jittered TTL out of +/-10%% bounds: %v", d)
		}
	}
}

func TestNamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	base := cachetest.NewMock()
	a := cache.Namespaced(base, "tenant:a")
	b := cache.Namespaced(base, "tenant:b")
	_ = a.Set(ctx, "k", []byte("av"), time.Minute)
	_ = b.Set(ctx, "k", []byte("bv"), time.Minute)
	av, _ := a.Get(ctx, "k")
	bv, _ := b.Get(ctx, "k")
	if string(av) != "av" || string(bv) != "bv" {
		t.Fatalf("namespace bleed: a=%q b=%q", av, bv)
	}
	if err := a.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.Has(ctx, "k"); ok {
		t.Fatal("a flush failed")
	}
	if ok, _ := b.Has(ctx, "k"); !ok {
		t.Fatal("a flush wrongly cleared b")
	}
}

func TestLockRefresh(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	l := cache.NewLock(c, "job", 60*time.Millisecond)
	if err := l.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Refresh(ctx); err != nil {
		t.Fatalf("refresh by owner should succeed: %v", err)
	}
	other := cache.NewLock(c, "job", time.Minute)
	if err := other.Refresh(ctx); err != cache.ErrLockNotAcquired {
		t.Fatalf("non-owner refresh must fail, got %v", err)
	}
}

func TestInstrumentCountsHitsAndMisses(t *testing.T) {
	ctx := context.Background()
	var hits, misses int64
	c := cache.Instrument(cachetest.NewMock(), cache.ObsHooks{
		Adapter: "mock",
		OnHit:   func(string) { atomic.AddInt64(&hits, 1) },
		OnMiss:  func(string) { atomic.AddInt64(&misses, 1) },
	})
	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	_, _ = c.Get(ctx, "k")
	_, _ = c.Get(ctx, "nope")
	if hits != 1 || misses != 1 {
		t.Fatalf("hooks: hits=%d misses=%d", hits, misses)
	}
	s := c.Stats()
	if s.Hits != 1 || s.Misses != 1 || s.Sets != 1 {
		t.Fatalf("instrumented stats wrong: %+v", s)
	}
}
