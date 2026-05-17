// cachetest.go — the shared cross-backend conformance suite (package cachetest, github.com/ubgo/cache/cachetest).
//
// Package role: cachetest is a sibling sub-package of github.com/ubgo/cache
// that every backend module imports to prove it satisfies the Cache
// contract. Its package doc lives in mock.go.
//
// This file: declares Factory and implements Run(t, factory) — the table of
// subtests every adapter executes from its own TestConformance. The WHY:
// the Cache interface's semantics (Get miss => ErrNotFound, ttl<=0 => no
// expiry, SetNX createonly, atomic Incr/Decr, ErrUnsupported for unservable
// ops) are enforced HERE, once, instead of re-tested per backend.
//
// AI-context: this is the executable form of cache.go's interface contract
// — adding a guarantee to the Cache docs means adding a subtest here, or
// backends can silently diverge. Each subtest builds a fresh empty cache
// via factory so subtests do not share state.

package cachetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
)

// Factory builds a fresh, empty cache for one subtest.
type Factory func(t *testing.T) cache.Cache

// Run executes the conformance suite against the adapter built by factory.
// Every ubgo cache adapter calls this from its own _test.go:
//
//	func TestConformance(t *testing.T) {
//	    cachetest.Run(t, func(t *testing.T) cache.Cache { return memcache.New() })
//	}
func Run(t *testing.T, factory Factory) {
	t.Helper()
	ctx := context.Background()

	t.Run("GetMissReturnsErrNotFound", func(t *testing.T) {
		c := factory(t)
		_, err := c.Get(ctx, "nope")
		if !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("SetGetRoundtrip", func(t *testing.T) {
		c := factory(t)
		if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
			t.Fatal(err)
		}
		got, err := c.Get(ctx, "k")
		if err != nil || string(got) != "v" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("SetOverwriteReplaces", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "k", []byte("a"), time.Minute)
		_ = c.Set(ctx, "k", []byte("b"), time.Minute)
		got, _ := c.Get(ctx, "k")
		if string(got) != "b" {
			t.Fatalf("want b, got %q", got)
		}
	})

	t.Run("TTLExpiry", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "k", []byte("v"), 80*time.Millisecond)
		if _, err := c.Get(ctx, "k"); err != nil {
			t.Fatalf("should be present pre-expiry: %v", err)
		}
		time.Sleep(160 * time.Millisecond)
		if _, err := c.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("want ErrNotFound post-expiry, got %v", err)
		}
	})

	t.Run("NoTTLPersists", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "k", []byte("v"), 0)
		d, err := c.TTL(ctx, "k")
		if err != nil {
			t.Fatal(err)
		}
		if d > 0 {
			t.Fatalf("want non-positive TTL for no-expiry, got %v", d)
		}
	})

	t.Run("HasReflectsPresence", func(t *testing.T) {
		c := factory(t)
		if ok, _ := c.Has(ctx, "k"); ok {
			t.Fatal("absent key reported present")
		}
		_ = c.Set(ctx, "k", []byte("v"), time.Minute)
		if ok, _ := c.Has(ctx, "k"); !ok {
			t.Fatal("present key reported absent")
		}
	})

	t.Run("MultiOps", func(t *testing.T) {
		c := factory(t)
		_ = c.SetMulti(ctx, map[string]cache.Item{
			"a": {Value: []byte("1"), TTL: time.Minute},
			"b": {Value: []byte("2"), TTL: time.Minute},
		})
		got, err := c.GetMulti(ctx, []string{"a", "b", "missing"})
		if err != nil {
			t.Fatal(err)
		}
		if string(got["a"]) != "1" || string(got["b"]) != "2" {
			t.Fatalf("bad multi get: %v", got)
		}
		if _, ok := got["missing"]; ok {
			t.Fatal("missing key should be absent from result")
		}
		_ = c.Del(ctx, "a", "b")
		if ok, _ := c.Has(ctx, "a"); ok {
			t.Fatal("del did not remove a")
		}
	})

	t.Run("SetNXFirstWins", func(t *testing.T) {
		c := factory(t)
		ok, err := c.SetNX(ctx, "k", []byte("first"), time.Minute)
		if err != nil || !ok {
			t.Fatalf("first SetNX should win: ok=%v err=%v", ok, err)
		}
		ok, err = c.SetNX(ctx, "k", []byte("second"), time.Minute)
		if err != nil || ok {
			t.Fatalf("second SetNX should fail: ok=%v err=%v", ok, err)
		}
		got, _ := c.Get(ctx, "k")
		if string(got) != "first" {
			t.Fatalf("value clobbered: %q", got)
		}
	})

	t.Run("ExpireResetsTTL", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "k", []byte("v"), time.Hour)
		if err := c.Expire(ctx, "k", 80*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		time.Sleep(160 * time.Millisecond)
		if _, err := c.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("want expired after Expire, got %v", err)
		}
	})

	t.Run("IncrDecrConcurrency", func(t *testing.T) {
		c := factory(t)
		const goroutines, per = 50, 200
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < per; j++ {
					if _, err := c.Incr(ctx, "ctr", 1); err != nil {
						t.Errorf("incr: %v", err)
						return
					}
				}
			}()
		}
		wg.Wait()
		got, err := c.Incr(ctx, "ctr", 0)
		if err != nil {
			t.Fatal(err)
		}
		if want := int64(goroutines * per); got != want {
			t.Fatalf("counter race: got %d want %d", got, want)
		}
		if v, _ := c.Decr(ctx, "ctr", 100); v != int64(goroutines*per)-100 {
			t.Fatalf("decr wrong: %d", v)
		}
	})

	t.Run("DeleteByPrefix", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "user:1", []byte("a"), time.Minute)
		_ = c.Set(ctx, "user:2", []byte("b"), time.Minute)
		_ = c.Set(ctx, "post:1", []byte("c"), time.Minute)
		if err := c.DeleteByPrefix(ctx, "user:"); err != nil {
			t.Fatal(err)
		}
		if ok, _ := c.Has(ctx, "user:1"); ok {
			t.Fatal("user:1 survived prefix delete")
		}
		if ok, _ := c.Has(ctx, "post:1"); !ok {
			t.Fatal("post:1 wrongly deleted")
		}
	})

	t.Run("FlushClears", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "k", []byte("v"), time.Minute)
		if err := c.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		if ok, _ := c.Has(ctx, "k"); ok {
			t.Fatal("flush did not clear")
		}
	})

	t.Run("IterationPrefixScoped", func(t *testing.T) {
		c := factory(t)
		_ = c.Set(ctx, "a:1", []byte("1"), time.Minute)
		_ = c.Set(ctx, "a:2", []byte("2"), time.Minute)
		_ = c.Set(ctx, "b:1", []byte("3"), time.Minute)
		it := c.Iterate(ctx, cache.IterateOpts{Prefix: "a:"})
		defer func() { _ = it.Close() }()
		seen := map[string]bool{}
		for it.Next() {
			seen[it.Key()] = true
		}
		if err := it.Err(); err != nil && !errors.Is(err, cache.ErrUnsupported) {
			t.Fatal(err)
		}
		if errors.Is(it.Err(), cache.ErrUnsupported) {
			t.Skip("adapter does not support Iterate")
		}
		if !seen["a:1"] || !seen["a:2"] || seen["b:1"] {
			t.Fatalf("iteration not prefix-scoped: %v", seen)
		}
	})

	t.Run("PingHealthy", func(t *testing.T) {
		c := factory(t)
		if err := c.Ping(ctx); err != nil {
			t.Fatalf("fresh cache should ping healthy: %v", err)
		}
	})

	t.Run("CloseIdempotent", func(t *testing.T) {
		c := factory(t)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("second Close must be a no-op, got %v", err)
		}
	})

	t.Run("RememberSingleFlight", func(t *testing.T) {
		c := factory(t)
		var calls int64
		var mu sync.Mutex
		load := func(ctx context.Context) (string, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			time.Sleep(30 * time.Millisecond)
			return "value", nil
		}
		var wg sync.WaitGroup
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := cache.Remember(ctx, c, "k", time.Minute, load)
				if err != nil || v != "value" {
					t.Errorf("remember: %q %v", v, err)
				}
			}()
		}
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 {
			t.Fatalf("single-flight failed: loader called %d times, want 1", calls)
		}
	})

	t.Run("LockMutualExclusion", func(t *testing.T) {
		c := factory(t)
		l1 := cache.NewLock(c, "job", time.Minute)
		l2 := cache.NewLock(c, "job", time.Minute)
		if err := l1.Acquire(ctx); err != nil {
			t.Fatalf("first acquire should succeed: %v", err)
		}
		if err := l2.Acquire(ctx); !errors.Is(err, cache.ErrLockNotAcquired) {
			t.Fatalf("second acquire should fail with ErrLockNotAcquired, got %v", err)
		}
		if err := l1.Release(ctx); err != nil {
			t.Fatal(err)
		}
		if err := l2.Acquire(ctx); err != nil {
			t.Fatalf("acquire after release should succeed: %v", err)
		}
	})
}

// Bench runs comparable benchmarks across adapters.
func Bench(b *testing.B, factory func(b *testing.B) cache.Cache) {
	ctx := context.Background()

	b.Run("Set", func(b *testing.B) {
		c := factory(b)
		v := []byte("benchmark-value")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = c.Set(ctx, key(i&1023), v, time.Minute)
		}
	})

	b.Run("GetHot", func(b *testing.B) {
		c := factory(b)
		_ = c.Set(ctx, "hot", []byte("v"), time.Hour)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Get(ctx, "hot")
		}
	})

	b.Run("GetCold", func(b *testing.B) {
		c := factory(b)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Get(ctx, key(i))
		}
	})

	b.Run("Remember", func(b *testing.B) {
		c := factory(b)
		load := func(ctx context.Context) (int, error) { return 1, nil }
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = cache.Remember(ctx, c, "k", time.Hour, load)
		}
	})
}

func key(i int) string { return fmt.Sprintf("key:%d", i) }
