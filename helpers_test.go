// helpers_test.go — tests for Memoize, QueryKey and Once (helpers.go).

package cache_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestMemoizeCachesPerArg(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var calls int64
	get := cache.Memoize(c, "sq", time.Minute,
		func(_ context.Context, n int) (int, error) {
			atomic.AddInt64(&calls, 1)
			return n * n, nil
		},
		strconv.Itoa)

	for i := 0; i < 3; i++ {
		if v, _ := get(ctx, 9); v != 81 {
			t.Fatalf("want 81, got %d", v)
		}
	}
	if v, _ := get(ctx, 4); v != 16 {
		t.Fatalf("want 16, got %d", v)
	}
	if calls != 2 { // one per distinct arg
		t.Fatalf("loader ran %d times, want 2", calls)
	}
}

func TestQueryKeyDeterministicAndUnambiguous(t *testing.T) {
	a := cache.QueryKey("q", "a", "bc")
	b := cache.QueryKey("q", "a", "bc")
	if a != b {
		t.Fatal("QueryKey not deterministic")
	}
	if cache.QueryKey("q", "a", "bc") == cache.QueryKey("q", "ab", "c") {
		t.Fatal("QueryKey collision between (a,bc) and (ab,c)")
	}
	if cache.QueryKey("q", 1) == cache.QueryKey("other", 1) {
		t.Fatal("different names must not collide")
	}
}

func TestOnceRunsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var runs int64
	op := func(_ context.Context) (string, error) {
		atomic.AddInt64(&runs, 1)
		return "charged", nil
	}
	for i := 0; i < 5; i++ {
		v, err := cache.Once(ctx, c, "pay:tx-1", time.Minute, op)
		if err != nil || v != "charged" {
			t.Fatalf("attempt %d: %q %v", i, v, err)
		}
	}
	if runs != 1 {
		t.Fatalf("idempotent op ran %d times, want 1", runs)
	}
}

func TestOnceFailureAllowsRetry(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	var n int64
	op := func(_ context.Context) (int, error) {
		if atomic.AddInt64(&n, 1) == 1 {
			return 0, errors.New("transient")
		}
		return 7, nil
	}
	if _, err := cache.Once(ctx, c, "k", time.Minute, op); err == nil {
		t.Fatal("first attempt should surface the error")
	}
	v, err := cache.Once(ctx, c, "k", time.Minute, op)
	if err != nil || v != 7 {
		t.Fatalf("retry after failure should succeed, got %d %v", v, err)
	}
}
