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

func TestWarmPreloadsConcurrently(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	keys := make([]string, 50)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}
	var peak, cur int64
	res := cache.Warm(ctx, c, keys, time.Minute, 8,
		func(_ context.Context, k string) (string, error) {
			n := atomic.AddInt64(&cur, 1)
			for {
				p := atomic.LoadInt64(&peak)
				if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt64(&cur, -1)
			return "v:" + k, nil
		})

	if len(res) != 50 {
		t.Fatalf("want 50 results, got %d", len(res))
	}
	for _, r := range res {
		if r.Err != nil {
			t.Fatalf("unexpected err for %s: %v", r.Key, r.Err)
		}
	}
	if peak > 8 {
		t.Fatalf("concurrency cap breached: peak %d > 8", peak)
	}
	v, err := cache.GetT[string](ctx, c, "k7")
	if err != nil || v != "v:k7" {
		t.Fatalf("warm did not populate cache: %q %v", v, err)
	}
}

func TestWarmReportsPerKeyErrors(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	res := cache.Warm(ctx, c, []string{"ok", "bad"}, time.Minute, 2,
		func(_ context.Context, k string) (int, error) {
			if k == "bad" {
				return 0, errors.New("load failed")
			}
			return 1, nil
		})
	byKey := map[string]error{}
	for _, r := range res {
		byKey[r.Key] = r.Err
	}
	if byKey["ok"] != nil {
		t.Fatalf("ok should succeed: %v", byKey["ok"])
	}
	if byKey["bad"] == nil {
		t.Fatal("bad key error should be reported, not fatal")
	}
}

func TestWarmHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	res := cache.Warm(ctx, cachetest.NewMock(), []string{"a", "b"}, time.Minute, 1,
		func(context.Context, string) (int, error) { return 1, nil })
	if len(res) != 2 || res[0].Err == nil {
		t.Fatalf("cancelled warm should mark keys with ctx error: %+v", res)
	}
}
