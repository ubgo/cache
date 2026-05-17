// hotkeys_test.go — tests for the Space-Saving HotKeyTracker (hotkeys.go).

package cache_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestHotKeyTrackerRanksHotKey(t *testing.T) {
	ctx := context.Background()
	hk := cache.NewHotKeyTracker(cachetest.NewMock(), 50)
	_ = hk.Set(ctx, "hot", []byte("v"), time.Minute)
	for i := 0; i < 200; i++ {
		_ = hk.Set(ctx, fmt.Sprintf("cold:%d", i), []byte("v"), time.Minute)
	}

	for i := 0; i < 500; i++ {
		_, _ = hk.Get(ctx, "hot")
	}
	for i := 0; i < 200; i++ {
		_, _ = hk.Get(ctx, fmt.Sprintf("cold:%d", i)) // each cold key once
	}

	top := hk.Top(1)
	if len(top) != 1 || top[0].Key != "hot" {
		t.Fatalf("hot key not ranked first: %+v", top)
	}
	if top[0].Est < 400 { // Space-Saving lower-bounds the true 500 well here
		t.Fatalf("hot estimate too low: %d", top[0].Est)
	}
}

func TestHotKeyTrackerBoundedMemory(t *testing.T) {
	ctx := context.Background()
	hk := cache.NewHotKeyTracker(cachetest.NewMock(), 16)
	for i := 0; i < 10_000; i++ {
		k := fmt.Sprintf("k:%d", i)
		_ = hk.Set(ctx, k, []byte("v"), time.Minute)
		_, _ = hk.Get(ctx, k)
	}
	if got := len(hk.Top(0)); got > 16 {
		t.Fatalf("tracker exceeded capacity: %d counters", got)
	}
}

func TestHotKeyTrackerResetAndConformance(t *testing.T) {
	// Still a valid cache.Cache.
	cachetest.Run(t, func(_ *testing.T) cache.Cache {
		return cache.NewHotKeyTracker(cachetest.NewMock(), 32)
	})

	ctx := context.Background()
	hk := cache.NewHotKeyTracker(cachetest.NewMock(), 8)
	_ = hk.Set(ctx, "k", []byte("v"), time.Minute)
	_, _ = hk.Get(ctx, "k")
	if len(hk.Top(0)) == 0 {
		t.Fatal("expected tracked keys before reset")
	}
	hk.Reset()
	if len(hk.Top(0)) != 0 {
		t.Fatal("Reset should clear counters")
	}
}
