// remember_multi_test.go — tests for batch load-through RememberMulti (remember_multi.go).

package cache_test

import (
	"context"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestRememberMultiLoadsMissesOnce(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()

	var calls int64
	var lastMiss []string
	load := func(_ context.Context, miss []string) (map[string]int, error) {
		atomic.AddInt64(&calls, 1)
		lastMiss = append([]string(nil), miss...)
		m := make(map[string]int, len(miss))
		for _, k := range miss {
			m[k] = len(k) // arbitrary derived value
		}
		return m, nil
	}

	// Warm one key so it is served from cache, not the loader.
	if err := cache.SetT(ctx, c, "bb", 2, time.Minute); err != nil {
		t.Fatal(err)
	}

	keys := []string{"a", "bb", "ccc"}
	got, err := cache.RememberMulti(ctx, c, keys, time.Minute, load)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 || got["bb"] != 2 || got["ccc"] != 3 {
		t.Fatalf("unexpected result: %v", got)
	}
	if calls != 1 {
		t.Fatalf("loader called %d times, want 1", calls)
	}
	sort.Strings(lastMiss)
	if len(lastMiss) != 2 || lastMiss[0] != "a" || lastMiss[1] != "ccc" {
		t.Fatalf("loader should only see misses, saw %v", lastMiss)
	}

	// Second call: everything cached, loader must not run.
	got2, err := cache.RememberMulti(ctx, c, keys, time.Minute, load)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("loader ran again (%d); all keys should be cached", calls)
	}
	if got2["ccc"] != 3 {
		t.Fatalf("cached decode wrong: %v", got2)
	}
}

func TestRememberMultiOmitsUnresolved(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	load := func(_ context.Context, miss []string) (map[string]string, error) {
		m := map[string]string{}
		for _, k := range miss {
			if k != "ghost" { // pretend "ghost" does not exist
				m[k] = "v:" + k
			}
		}
		return m, nil
	}
	got, err := cache.RememberMulti(ctx, c, []string{"x", "ghost"}, time.Minute, load)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ghost"]; ok {
		t.Fatal("unresolved key must be omitted")
	}
	if got["x"] != "v:x" {
		t.Fatalf("got %v", got)
	}
	// "ghost" was never stored, so a re-fetch still misses it.
	if _, err := cache.GetT[string](ctx, c, "ghost"); err != cache.ErrNotFound {
		t.Fatalf("ghost should not be cached, got %v", err)
	}
}

func TestRememberMultiEmpty(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	got, err := cache.RememberMulti(ctx, c, nil, time.Minute,
		func(context.Context, []string) (map[string]int, error) {
			t.Fatal("loader must not run for empty keys")
			return nil, nil
		})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty keys: %v %v", got, err)
	}
}
