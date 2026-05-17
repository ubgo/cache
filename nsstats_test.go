package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestNamespaceStatsBucketsByPrefix(t *testing.T) {
	ctx := context.Background()
	ns := cache.NewNamespaceStats(cachetest.NewMock(), nil)

	_ = ns.Set(ctx, "user:1", []byte("a"), time.Minute)
	_ = ns.Set(ctx, "post:1", []byte("b"), time.Minute)

	_, _ = ns.Get(ctx, "user:1")       // user hit
	_, _ = ns.Get(ctx, "user:missing") // user miss
	_, _ = ns.Get(ctx, "post:1")       // post hit

	by := ns.ByNamespace()
	u, p := by["user"], by["post"]
	if u.Hits != 1 || u.Misses != 1 || u.Sets != 1 {
		t.Fatalf("user bucket wrong: %+v", u)
	}
	if p.Hits != 1 || p.Misses != 0 || p.Sets != 1 {
		t.Fatalf("post bucket wrong: %+v", p)
	}
	if u.HitRatio() != 0.5 {
		t.Fatalf("user hit ratio: %v", u.HitRatio())
	}
}

func TestNamespaceStatsCustomFn(t *testing.T) {
	ctx := context.Background()
	ns := cache.NewNamespaceStats(cachetest.NewMock(),
		func(string) string { return "all" })
	_ = ns.Set(ctx, "a", []byte("1"), time.Minute)
	_ = ns.Set(ctx, "b", []byte("2"), time.Minute)
	by := ns.ByNamespace()
	if len(by) != 1 || by["all"].Sets != 2 {
		t.Fatalf("custom nsFn not honored: %+v", by)
	}
}

func TestNamespaceStatsGetMultiAndDel(t *testing.T) {
	ctx := context.Background()
	ns := cache.NewNamespaceStats(cachetest.NewMock(), nil)
	_ = ns.Set(ctx, "x:1", []byte("v"), time.Minute)

	res, err := ns.GetMulti(ctx, []string{"x:1", "x:2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("getmulti result: %v", res)
	}
	if err := ns.Del(ctx, "x:1"); err != nil {
		t.Fatal(err)
	}
	x := ns.ByNamespace()["x"]
	if x.Hits != 1 || x.Misses != 1 || x.Deletes != 1 {
		t.Fatalf("x bucket wrong: %+v", x)
	}
}

func TestNamespaceStatsStillConforms(t *testing.T) {
	cachetest.Run(t, func(_ *testing.T) cache.Cache {
		return cache.NewNamespaceStats(cachetest.NewMock(), nil)
	})
}
