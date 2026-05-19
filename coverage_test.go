// coverage_test.go — focused tests for branches the behavioral suites do not
// exercise: codec Name/Encode/Decode error paths, EncryptedCodec edge cases,
// every Namespaced wrapped method, Typed[T] delegation, Locker error paths,
// obs.Del, GetT envelope fallthrough, RememberMulti error/dup paths, and the
// remaining stats/jitter/hardTTL branches. Test-only; no production changes.

package cache_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

// ---------- codecs ----------

func TestCodecNamesAndErrorPaths(t *testing.T) {
	if (cache.JSONCodec{}).Name() != "json" {
		t.Fatal("json name")
	}
	if (cache.GobCodec{}).Name() != "gob" {
		t.Fatal("gob name")
	}
	if (cache.RawCodec{}).Name() != "raw" {
		t.Fatal("raw name")
	}

	// JSON encode failure (channels are not JSON-serializable).
	if _, err := (cache.JSONCodec{}).Encode(make(chan int)); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("json encode err: %v", err)
	}
	// JSON decode failure.
	var n int
	if err := (cache.JSONCodec{}).Decode([]byte("not-json"), &n); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("json decode err: %v", err)
	}

	gc := cache.GobCodec{}
	b, err := gc.Encode(7)
	if err != nil {
		t.Fatal(err)
	}
	var got int
	if err := gc.Decode(b, &got); err != nil || got != 7 {
		t.Fatalf("gob roundtrip: %d %v", got, err)
	}
	// Gob encode failure: gob cannot encode a func value.
	if _, err := gc.Encode(func() {}); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("gob encode err: %v", err)
	}
	// Gob decode failure: garbage bytes.
	if err := gc.Decode([]byte("garbage"), &got); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("gob decode err: %v", err)
	}

	rc := cache.RawCodec{}
	if out, err := rc.Encode([]byte("x")); err != nil || string(out) != "x" {
		t.Fatalf("raw []byte: %q %v", out, err)
	}
	bs := []byte("y")
	if out, err := rc.Encode(&bs); err != nil || string(out) != "y" {
		t.Fatalf("raw *[]byte: %q %v", out, err)
	}
	if out, err := rc.Encode("z"); err != nil || string(out) != "z" {
		t.Fatalf("raw string: %q %v", out, err)
	}
	s := "w"
	if out, err := rc.Encode(&s); err != nil || string(out) != "w" {
		t.Fatalf("raw *string: %q %v", out, err)
	}
	if _, err := rc.Encode(42); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("raw encode unsupported: %v", err)
	}
	var rb []byte
	if err := rc.Decode([]byte("d"), &rb); err != nil || string(rb) != "d" {
		t.Fatalf("raw decode []byte: %q %v", rb, err)
	}
	var rs string
	if err := rc.Decode([]byte("e"), &rs); err != nil || rs != "e" {
		t.Fatalf("raw decode string: %q %v", rs, err)
	}
	if err := rc.Decode([]byte("d"), &n); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("raw decode unsupported: %v", err)
	}
}

// ---------- EncryptedCodec ----------

func TestEncryptedCodecNilKeyAndDefaults(t *testing.T) {
	// Nil Key provider => gcm() error surfaced through Encode and Decode.
	ec := cache.EncryptedCodec{Inner: cache.JSONCodec{}}
	if _, err := ec.Encode(1); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("nil key encode: %v", err)
	}
	if err := ec.Decode([]byte("xxxxxxxxxxxxxxxx"), new(int)); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("nil key decode: %v", err)
	}

	// Nil Inner falls back to DefaultCodec; Name reflects inner.
	ec2 := cache.EncryptedCodec{Key: cache.StaticKey(make([]byte, 32))}
	if !strings.HasSuffix(ec2.Name(), "json") {
		t.Fatalf("default inner name: %s", ec2.Name())
	}
	b, err := ec2.Encode("hello")
	if err != nil {
		t.Fatal(err)
	}
	var out string
	if err := ec2.Decode(b, &out); err != nil || out != "hello" {
		t.Fatalf("default-inner roundtrip: %q %v", out, err)
	}

	// Wrong key size => aes.NewCipher error.
	ecBad := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: cache.StaticKey([]byte("short"))}
	if _, err := ecBad.Encode(1); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("bad key size encode: %v", err)
	}

	// Key provider returns an error.
	ecErr := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: func() ([]byte, error) {
		return nil, errors.New("kms down")
	}}
	if _, err := ecErr.Encode(1); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("key provider err encode: %v", err)
	}

	// Ciphertext shorter than nonce.
	ecOK := cache.EncryptedCodec{Inner: cache.JSONCodec{}, Key: cache.StaticKey(make([]byte, 16))}
	if err := ecOK.Decode([]byte("short"), new(int)); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("short ciphertext: %v", err)
	}

	// Inner encode failure propagates (channel not serializable).
	if _, err := ecOK.Encode(make(chan int)); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("inner encode err: %v", err)
	}
}

// ---------- Namespaced: every wrapped method ----------

func TestNamespacedAllMethods(t *testing.T) {
	ctx := context.Background()
	base := cachetest.NewMock()
	ns := cache.Namespaced(base, "svc") // no trailing colon -> appended

	if err := ns.Set(ctx, "a", []byte("1"), time.Minute); err != nil {
		t.Fatal(err)
	}
	// Underlying key is prefixed.
	if v, err := base.Get(ctx, "svc:a"); err != nil || string(v) != "1" {
		t.Fatalf("prefix not applied: %q %v", v, err)
	}
	if v, err := ns.Get(ctx, "a"); err != nil || string(v) != "1" {
		t.Fatalf("ns get: %q %v", v, err)
	}
	if ok, _ := ns.Has(ctx, "a"); !ok {
		t.Fatal("ns has")
	}
	if d, err := ns.TTL(ctx, "a"); err != nil || d <= 0 {
		t.Fatalf("ns ttl: %v %v", d, err)
	}
	if err := ns.SetMulti(ctx, map[string]cache.Item{
		"m1": {Value: []byte("x"), TTL: time.Minute},
		"m2": {Value: []byte("y"), TTL: time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	gm, err := ns.GetMulti(ctx, []string{"m1", "m2", "nope"})
	if err != nil || string(gm["m1"]) != "x" || string(gm["m2"]) != "y" {
		t.Fatalf("ns getmulti: %v %v", gm, err)
	}
	if _, dup := gm["nope"]; dup {
		t.Fatal("missing key present")
	}
	ok, err := ns.SetNX(ctx, "nx", []byte("v"), time.Minute)
	if err != nil || !ok {
		t.Fatalf("ns setnx: %v %v", ok, err)
	}
	if err := ns.Expire(ctx, "nx", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := ns.Touch(ctx, "nx"); err != nil {
		t.Fatal(err)
	}
	if v, err := ns.Incr(ctx, "ctr", 5); err != nil || v != 5 {
		t.Fatalf("ns incr: %d %v", v, err)
	}
	if v, err := ns.Decr(ctx, "ctr", 2); err != nil || v != 3 {
		t.Fatalf("ns decr: %d %v", v, err)
	}
	if err := ns.Del(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ns.Has(ctx, "a"); ok {
		t.Fatal("ns del failed")
	}
	_ = ns.Set(ctx, "p:1", []byte("z"), time.Minute)
	if err := ns.DeleteByPrefix(ctx, "p:"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := ns.Has(ctx, "p:1"); ok {
		t.Fatal("ns deletebyprefix failed")
	}

	// Iterate strips prefix back off via nsIter.Key().
	_ = ns.Set(ctx, "it:1", []byte("a"), time.Minute)
	_ = ns.Set(ctx, "it:2", []byte("b"), time.Minute)
	it := ns.Iterate(ctx, cache.IterateOpts{Prefix: "it:"})
	seen := map[string]bool{}
	for it.Next() {
		seen[it.Key()] = true
	}
	_ = it.Close()
	if !seen["it:1"] || !seen["it:2"] {
		t.Fatalf("ns iterate keys not unprefixed: %v", seen)
	}

	// Flush on prefixed view scopes to prefix.
	_ = base.Set(ctx, "other:keep", []byte("k"), time.Minute)
	if err := ns.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := base.Has(ctx, "other:keep"); !ok {
		t.Fatal("ns flush wiped non-namespaced key")
	}

	// Empty prefix => Flush falls back to real Flush.
	nsEmpty := cache.Namespaced(base, "")
	_ = nsEmpty.Set(ctx, "g", []byte("v"), time.Minute)
	if err := nsEmpty.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := base.Has(ctx, "other:keep"); ok {
		t.Fatal("empty-prefix flush should wipe everything")
	}

	// GetMulti error path.
	failing := cachetest.NewMock()
	failing.FailOn = map[string]error{"getmulti": errors.New("boom")}
	nsF := cache.Namespaced(failing, "x")
	if _, err := nsF.GetMulti(ctx, []string{"a"}); err == nil {
		t.Fatal("ns getmulti should surface backend error")
	}
}

// ---------- Typed[T] ----------

func TestTypedDelegation(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	type item struct{ N int }
	tv := cache.NewTyped[item](c, cache.WithJitter(0))
	if tv.Raw() == nil {
		t.Fatal("Raw nil")
	}
	if err := tv.Set(ctx, "k", item{N: 9}, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := tv.Get(ctx, "k")
	if err != nil || got.N != 9 {
		t.Fatalf("typed get: %+v %v", got, err)
	}
	var loads int
	v, err := tv.Remember(ctx, "r", time.Minute, func(context.Context) (item, error) {
		loads++
		return item{N: 1}, nil
	})
	if err != nil || v.N != 1 {
		t.Fatalf("typed remember: %+v %v", v, err)
	}
	if err := tv.Del(ctx, "k", "r"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Has(ctx, "k"); ok {
		t.Fatal("typed del failed")
	}
}

// ---------- Locker error / ownership paths ----------

func TestLockerErrorPaths(t *testing.T) {
	ctx := context.Background()

	// Acquire surfaces backend error.
	fail := cachetest.NewMock()
	fail.FailOn = map[string]error{"setnx": errors.New("backend")}
	if err := cache.NewLock(fail, "j", time.Minute).Acquire(ctx); err == nil {
		t.Fatal("acquire should surface backend error")
	}

	// owns() backend error via Get failure -> Refresh/Release surface it.
	fg := cachetest.NewMock()
	l := cache.NewLock(fg, "j", time.Minute)
	if err := l.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	fg.FailOn = map[string]error{"get": errors.New("get-fail")}
	if err := l.Refresh(ctx); err == nil {
		t.Fatal("refresh should surface owns() error")
	}
	if err := l.Release(ctx); err == nil {
		t.Fatal("release should surface owns() error")
	}

	// Release when not owned (key absent) -> nil, nothing to release.
	c := cachetest.NewMock()
	l2 := cache.NewLock(c, "j2", time.Minute)
	if err := l2.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	_ = c.Flush(ctx) // key gone -> owns() returns false via ErrNotFound
	if err := l2.Release(ctx); err != nil {
		t.Fatalf("release of vanished lock should be nil, got %v", err)
	}

	// Release by a non-owner whose token differs but key exists.
	c2 := cachetest.NewMock()
	owner := cache.NewLock(c2, "j3", time.Minute)
	if err := owner.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	stranger := cache.NewLock(c2, "j3", time.Minute)
	if err := stranger.Release(ctx); err != nil {
		t.Fatalf("non-owner release should be a no-op nil, got %v", err)
	}
	// Owner still holds it.
	if err := owner.Refresh(ctx); err != nil {
		t.Fatalf("owner should still hold lock: %v", err)
	}
}

// ---------- obs.Del + Del with no keys ----------

func TestInstrumentDelAndEmptyKeys(t *testing.T) {
	ctx := context.Background()
	var ops int
	c := cache.Instrument(cachetest.NewMock(), cache.ObsHooks{
		OnOp: func(cache.OpEvent) { ops++ },
	})
	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := c.Del(ctx); err != nil { // no keys -> k=="" branch
		t.Fatal(err)
	}
	if ops < 3 {
		t.Fatalf("expected set+del+del events, got %d", ops)
	}
	s := c.Stats()
	if s.Deletes != 1 {
		t.Fatalf("deletes counter: %d", s.Deletes)
	}
}

// ---------- GetT: envelope decode-failure fallthrough + Missing + errors ----------

func TestGetTEnvelopeAndErrors(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()

	// Plain (non-envelope) payload written by SetT decodes via plain codec.
	if err := cache.SetT(ctx, c, "p", 123, time.Minute); err != nil {
		t.Fatal(err)
	}
	v, err := cache.GetT[int](ctx, c, "p")
	if err != nil || v != 123 {
		t.Fatalf("plain GetT: %d %v", v, err)
	}

	// Non-envelope, undecodable for target type -> decode error.
	_ = c.Set(ctx, "bad", []byte("not-an-int"), time.Minute)
	if _, err := cache.GetT[int](ctx, c, "bad"); err == nil {
		t.Fatal("expected decode error for non-envelope garbage")
	}

	// Miss -> ErrNotFound.
	if _, err := cache.GetT[int](ctx, c, "absent"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("GetT miss: %v", err)
	}

	// Missing-sentinel envelope (negative cache) -> ErrNotFound.
	var loads int
	_, _ = cache.Remember(ctx, c, "neg", time.Minute, func(context.Context) (int, error) {
		loads++
		return 0, cache.ErrNotFound
	}, cache.WithNegativeTTL(time.Minute))
	if _, err := cache.GetT[int](ctx, c, "neg"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("GetT on negative envelope: %v", err)
	}

	// Envelope present but inner data undecodable for target type.
	if _, err := cache.Remember(ctx, c, "env", time.Minute,
		func(context.Context) (string, error) { return "text", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetT[int](ctx, c, "env"); err == nil {
		t.Fatal("expected decode error reading string envelope as int")
	}

	// SetT encode failure.
	if err := cache.SetT(ctx, c, "ch", make(chan int), time.Minute); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("SetT encode failure: %v", err)
	}

	// GetT backend (non-NotFound) error propagates.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"get": errors.New("io")}
	if _, err := cache.GetT[int](ctx, fc, "x"); err == nil {
		t.Fatal("GetT should surface backend error")
	}
}

// ---------- Remember: backend error, encode error, loadAndStore negative w/o ttl ----------

func TestRememberErrorBranches(t *testing.T) {
	ctx := context.Background()

	// Backend Get error (non-NotFound) bubbles immediately.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"get": errors.New("io")}
	if _, err := cache.Remember(ctx, fc, "k", time.Minute,
		func(context.Context) (int, error) { return 1, nil }); err == nil {
		t.Fatal("Remember should surface backend Get error")
	}

	// Loader returns a non-NotFound error -> surfaced.
	c := cachetest.NewMock()
	sentinel := errors.New("loader boom")
	if _, err := cache.Remember(ctx, c, "k", time.Minute,
		func(context.Context) (int, error) { return 0, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("loader error: %v", err)
	}

	// Loader value codec-encode failure -> surfaced as serialization error.
	if _, err := cache.Remember(ctx, c, "ch", time.Minute,
		func(context.Context) (chan int, error) { return make(chan int), nil }); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("encode failure in loadAndStore: %v", err)
	}

	// ttl<=0 path: stored permanently (Soft = +100y), still served from cache.
	var n int
	for i := 0; i < 3; i++ {
		v, err := cache.Remember(ctx, c, "perm", 0,
			func(context.Context) (int, error) { n++; return 5, nil })
		if err != nil || v != 5 {
			t.Fatalf("perm remember: %d %v", v, err)
		}
	}
	if n != 1 {
		t.Fatalf("ttl<=0 should still cache, loader ran %d times", n)
	}

	// staleIfError set but no usable stale value -> original error returned.
	c2 := cachetest.NewMock()
	if _, err := cache.Remember(ctx, c2, "se", time.Minute,
		func(context.Context) (int, error) { return 0, sentinel },
		cache.WithStaleIfError(time.Minute)); !errors.Is(err, sentinel) {
		t.Fatalf("stale-if-error with no stale value: %v", err)
	}

	// Negative caching disabled (negativeTTL==0): loader error with ErrNotFound
	// returns ErrNotFound, not cached.
	c3 := cachetest.NewMock()
	if _, err := cache.Remember(ctx, c3, "nf", time.Minute,
		func(context.Context) (int, error) { return 0, cache.ErrNotFound }); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("loader ErrNotFound: %v", err)
	}
}

// ---------- RememberMulti: dup keys, undecodable cached, loader error, encode error ----------

func TestRememberMultiBranches(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()

	// Dup input keys: loader called once with deduped missing slice.
	var gotMissing []string
	res, err := cache.RememberMulti(ctx, c, []string{"a", "a", "b"}, time.Minute,
		func(_ context.Context, miss []string) (map[string]int, error) {
			gotMissing = append([]string(nil), miss...)
			return map[string]int{"a": 1, "b": 2}, nil
		})
	if err != nil || res["a"] != 1 || res["b"] != 2 {
		t.Fatalf("rmulti: %v %v", res, err)
	}
	if len(gotMissing) != 2 {
		t.Fatalf("dup keys not deduped: %v", gotMissing)
	}

	// Cached hit path on re-call (no loader).
	called := false
	res, err = cache.RememberMulti(ctx, c, []string{"a"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { called = true; return nil, nil })
	if err != nil || res["a"] != 1 || called {
		t.Fatalf("cached path: %v %v called=%v", res, err, called)
	}

	// Undecodable cached bytes -> treated as miss and reloaded.
	_ = c.Set(ctx, "u", []byte("not-int"), time.Minute)
	res, err = cache.RememberMulti(ctx, c, []string{"u"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return map[string]int{"u": 7}, nil })
	if err != nil || res["u"] != 7 {
		t.Fatalf("undecodable reload: %v %v", res, err)
	}

	// Empty keys -> empty map, no error.
	res, err = cache.RememberMulti(ctx, c, nil, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return nil, nil })
	if err != nil || len(res) != 0 {
		t.Fatalf("empty: %v %v", res, err)
	}

	// GetMulti backend error.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"getmulti": errors.New("gm")}
	if _, err := cache.RememberMulti(ctx, fc, []string{"x"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return nil, nil }); err == nil {
		t.Fatal("rmulti should surface GetMulti error")
	}

	// Loader error.
	if _, err := cache.RememberMulti(ctx, cachetest.NewMock(), []string{"x"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return nil, errors.New("load") }); err == nil {
		t.Fatal("rmulti should surface loader error")
	}

	// Encode failure on a loaded value.
	if _, err := cache.RememberMulti(ctx, cachetest.NewMock(), []string{"x"}, time.Minute,
		func(context.Context, []string) (map[string]chan int, error) {
			return map[string]chan int{"x": make(chan int)}, nil
		}); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("rmulti encode failure: %v", err)
	}

	// SetMulti backend error after successful load.
	sf := cachetest.NewMock()
	sf.FailOn = map[string]error{"setmulti": errors.New("sm")}
	if _, err := cache.RememberMulti(ctx, sf, []string{"x"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return map[string]int{"x": 1}, nil }); err == nil {
		t.Fatal("rmulti should surface SetMulti error")
	}

	// Loader resolves nothing -> items empty, returns out (no SetMulti call).
	res, err = cache.RememberMulti(ctx, cachetest.NewMock(), []string{"none"}, time.Minute,
		func(context.Context, []string) (map[string]int, error) { return map[string]int{}, nil })
	if err != nil || len(res) != 0 {
		t.Fatalf("loader resolved nothing: %v %v", res, err)
	}
}

// ---------- Once: in-flight, get-error, decode-failure-on-cached ----------

func TestOnceBranches(t *testing.T) {
	ctx := context.Background()

	// Get backend error (non-NotFound) on result key -> surfaced.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"get": errors.New("io")}
	if _, err := cache.Once(ctx, fc, "k", time.Minute,
		func(context.Context) (int, error) { return 1, nil }); err == nil {
		t.Fatal("Once should surface backend Get error")
	}

	// SetNX backend error -> surfaced.
	sf := cachetest.NewMock()
	sf.FailOn = map[string]error{"setnx": errors.New("nx")}
	if _, err := cache.Once(ctx, sf, "k", time.Minute,
		func(context.Context) (int, error) { return 1, nil }); err == nil {
		t.Fatal("Once should surface SetNX error")
	}

	// Lease already held by someone else, no result cached yet -> ErrInFlight.
	c := cachetest.NewMock()
	if _, err := c.SetNX(ctx, "idemp:lock:k", []byte{1}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Once(ctx, c, "k", time.Minute,
		func(context.Context) (int, error) { return 9, nil }); !errors.Is(err, cache.ErrInFlight) {
		t.Fatalf("want ErrInFlight, got %v", err)
	}

	// Lease held by other but result already cached -> returns cached.
	if err := cache.SetT(ctx, c, "idemp:res:k2", 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	// store an encoded int 55 at result key the way Once does (DefaultCodec).
	b, _ := cache.JSONCodec{}.Encode(55)
	_ = c.Set(ctx, "idemp:res:k2", b, time.Minute)
	_, _ = c.SetNX(ctx, "idemp:lock:k2", []byte{1}, time.Minute)
	v, err := cache.Once(ctx, c, "k2", time.Minute,
		func(context.Context) (int, error) { return -1, nil })
	if err != nil || v != 55 {
		t.Fatalf("Once should return cached result, got %d %v", v, err)
	}
}

// ---------- Stats.HitRatio zero-traffic ----------

func TestStatsHitRatioZero(t *testing.T) {
	if (cache.Stats{}).HitRatio() != 0 {
		t.Fatal("zero traffic hit ratio must be 0")
	}
	s := cache.Stats{Hits: 3, Misses: 1}
	if s.HitRatio() != 0.75 {
		t.Fatalf("hit ratio: %v", s.HitRatio())
	}
}

// ---------- hardTTL permanent + applyJitter clamps ----------

func TestSetTPermanentNoExpiry(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	if err := cache.SetT(ctx, c, "k", 1, 0, cache.WithJitter(0.5)); err != nil {
		t.Fatal(err)
	}
	d, err := c.TTL(ctx, "k")
	if err != nil || d > 0 {
		t.Fatalf("ttl<=0 with jitter must stay no-expiry, got %v %v", d, err)
	}
}

// ---------- WithCodec option applied ----------

func TestWithCodecOption(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	if err := cache.SetT(ctx, c, "k", []byte("raw-bytes"), time.Minute,
		cache.WithCodec(cache.RawCodec{})); err != nil {
		t.Fatal(err)
	}
	got, err := cache.GetT[[]byte](ctx, c, "k", cache.WithCodec(cache.RawCodec{}))
	if err != nil || string(got) != "raw-bytes" {
		t.Fatalf("WithCodec roundtrip: %q %v", got, err)
	}
}
