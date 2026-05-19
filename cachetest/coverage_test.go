// coverage_test.go — exercises cachetest's own helpers: Bench (via
// testing.Benchmark with a tiny N), the Zipfian/ScanGen/HitRate/Round
// workload generators, and the Mock paths the conformance suite does not hit
// (SetClock, FailOn per op, Iterator Value, Stats, eviction-skipping Next).
// Test-only.

package cachetest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestBenchRunsViaTestingBenchmark(t *testing.T) {
	// Drive Bench's sub-benchmarks with a controlled tiny N.
	res := testing.Benchmark(func(b *testing.B) {
		cachetest.Bench(b, func(_ *testing.B) cache.Cache { return cachetest.NewMock() })
	})
	if res.N < 1 {
		t.Fatalf("benchmark did not run: N=%d", res.N)
	}
}

func TestZipfianAndWorkloadGenerators(t *testing.T) {
	z := cachetest.NewZipfian(1000, 1.2, 42)
	k1 := z.Key()
	if len(k1) == 0 || k1[:2] != "k:" {
		t.Fatalf("zipf key shape: %q", k1)
	}
	// Determinism: same seed -> same first key.
	z2 := cachetest.NewZipfian(1000, 1.2, 42)
	if z2.Key() != k1 {
		t.Fatal("zipfian not deterministic for a fixed seed")
	}

	c := cachetest.NewMock()
	hr := cachetest.HitRate(c, z.Key, 500)
	if hr < 0 || hr > 1 {
		t.Fatalf("hit rate out of range: %v", hr)
	}
	// ops==0 -> 0.
	if cachetest.HitRate(c, z.Key, 0) != 0 {
		t.Fatal("HitRate(0 ops) must be 0")
	}

	gen := cachetest.ScanGen(8, 3, 7)
	hot, scan := 0, 0
	for i := 0; i < 200; i++ {
		k := gen()
		switch k[:4] {
		case "hot:":
			hot++
		case "scan":
			scan++
		default:
			t.Fatalf("unexpected scan-gen key: %q", k)
		}
	}
	if hot == 0 || scan == 0 {
		t.Fatalf("ScanGen should mix hot and scan keys: hot=%d scan=%d", hot, scan)
	}

	if cachetest.Round(0.123456) != 0.1235 {
		t.Fatalf("Round: %v", cachetest.Round(0.123456))
	}
}

func TestMockSetClockDeterministicExpiry(t *testing.T) {
	ctx := context.Background()
	m := cachetest.NewMock()
	now := time.Unix(1_000_000, 0)
	m.SetClock(func() time.Time { return now })
	if err := m.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "k"); err != nil {
		t.Fatalf("should be live at frozen now: %v", err)
	}
	d, err := m.TTL(ctx, "k")
	if err != nil || d != time.Minute {
		t.Fatalf("frozen-clock TTL: %v %v", d, err)
	}
	now = now.Add(2 * time.Minute) // advance clock past expiry
	if _, err := m.Get(ctx, "k"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expired under advanced clock: %v", err)
	}
}

func TestMockFailOnEveryOp(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("injected")
	ops := []string{
		"get", "getmulti", "has", "ttl", "set", "setmulti", "setnx",
		"expire", "incr", "del", "deletebyprefix", "flush", "ping",
	}
	for _, op := range ops {
		m := cachetest.NewMock()
		m.FailOn = map[string]error{op: boom}
		var err error
		switch op {
		case "get":
			_, err = m.Get(ctx, "k")
		case "getmulti":
			_, err = m.GetMulti(ctx, []string{"k"})
		case "has":
			_, err = m.Has(ctx, "k")
		case "ttl":
			_, err = m.TTL(ctx, "k")
		case "set":
			err = m.Set(ctx, "k", []byte("v"), time.Minute)
		case "setmulti":
			err = m.SetMulti(ctx, map[string]cache.Item{"k": {Value: []byte("v")}})
		case "setnx":
			_, err = m.SetNX(ctx, "k", []byte("v"), time.Minute)
		case "expire":
			err = m.Expire(ctx, "k", time.Minute)
		case "incr":
			_, err = m.Incr(ctx, "k", 1)
		case "del":
			err = m.Del(ctx, "k")
		case "deletebyprefix":
			err = m.DeleteByPrefix(ctx, "k")
		case "flush":
			err = m.Flush(ctx)
		case "ping":
			err = m.Ping(ctx)
		}
		if !errors.Is(err, boom) {
			t.Fatalf("FailOn[%q] not honored: %v", op, err)
		}
		// Decr shares the incr op key.
		if op == "incr" {
			if _, e := m.Decr(ctx, "k", 1); !errors.Is(e, boom) {
				t.Fatalf("Decr should honor incr FailOn: %v", e)
			}
		}
	}

	// Closed mock -> ErrClosed for every op.
	m := cachetest.NewMock()
	_ = m.Close()
	if _, err := m.Get(ctx, "k"); !errors.Is(err, cache.ErrClosed) {
		t.Fatalf("closed Get: %v", err)
	}
}

func TestMockExpireMissingAndCounterPreservesExpiry(t *testing.T) {
	ctx := context.Background()
	m := cachetest.NewMock()
	// Expire on absent key -> ErrNotFound.
	if err := m.Expire(ctx, "absent", time.Minute); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expire missing: %v", err)
	}
	// Touch (calls Expire) on absent key -> ErrNotFound.
	if err := m.Touch(ctx, "absent"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("touch missing: %v", err)
	}
	// Touch on a present key succeeds.
	_ = m.Set(ctx, "k", []byte("v"), time.Minute)
	if err := m.Touch(ctx, "k"); err != nil {
		t.Fatalf("touch present: %v", err)
	}
}

func TestMockIteratorValueAndSkipExpired(t *testing.T) {
	ctx := context.Background()
	m := cachetest.NewMock()
	now := time.Unix(2_000_000, 0)
	m.SetClock(func() time.Time { return now })
	_ = m.Set(ctx, "a:1", []byte("one"), time.Minute)
	_ = m.Set(ctx, "a:2", []byte("two"), 10*time.Second) // will expire
	_ = m.Set(ctx, "b:1", []byte("three"), time.Minute)

	it := m.Iterate(ctx, cache.IterateOpts{Prefix: "a:"})
	// Expire a:2 between snapshot and iteration so Next() skips it.
	now = now.Add(30 * time.Second)
	vals := map[string]string{}
	for it.Next() {
		vals[it.Key()] = string(it.Value())
	}
	if it.Err() != nil {
		t.Fatalf("iter err: %v", it.Err())
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if vals["a:1"] != "one" {
		t.Fatalf("a:1 value wrong: %v", vals)
	}
	if _, ok := vals["a:2"]; ok {
		t.Fatal("expired a:2 should be skipped during iteration")
	}
}

func TestMockTTLNoExpiry(t *testing.T) {
	ctx := context.Background()
	m := cachetest.NewMock()
	_ = m.Set(ctx, "k", []byte("v"), 0) // ttl<=0 => no expiry
	d, err := m.TTL(ctx, "k")
	if err != nil || d != 0 {
		t.Fatalf("no-expiry TTL must be 0, got %v %v", d, err)
	}
}

func TestMockStatsLiveCount(t *testing.T) {
	ctx := context.Background()
	m := cachetest.NewMock()
	if m.Stats().Entries != 0 {
		t.Fatal("empty mock entries != 0")
	}
	_ = m.Set(ctx, "a", []byte("1"), time.Minute)
	_ = m.Set(ctx, "b", []byte("2"), time.Minute)
	if m.Stats().Entries != 2 {
		t.Fatalf("entries: %d", m.Stats().Entries)
	}
}
