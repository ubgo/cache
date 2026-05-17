// Package cachetest provides the shared conformance suite every cache adapter
// runs, plus a correct in-memory Mock for consumer unit tests.
package cachetest

import (
	"context"
	"encoding/binary"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ubgo/cache"
)

// Mock is a correct, dependency-free in-memory cache.Cache for unit tests.
// It passes the conformance suite, so it is also the reference implementation.
// FailOn injects an error from every op whose name matches (e.g. "get").
type Mock struct {
	mu     sync.Mutex
	data   map[string]mockEntry
	closed bool

	FailOn map[string]error // op name -> error to return
	now    func() time.Time // overridable clock for deterministic TTL tests
}

type mockEntry struct {
	val []byte
	exp time.Time // zero = no expiry
}

// NewMock returns an empty Mock.
func NewMock() *Mock {
	return &Mock{data: map[string]mockEntry{}, now: time.Now}
}

// SetClock overrides the clock (tests only).
func (m *Mock) SetClock(fn func() time.Time) { m.mu.Lock(); m.now = fn; m.mu.Unlock() }

func (m *Mock) fail(op string) error {
	if m.closed {
		return cache.ErrClosed
	}
	if e, ok := m.FailOn[op]; ok {
		return e
	}
	return nil
}

// caller must hold m.mu.
func (m *Mock) live(key string) (mockEntry, bool) {
	e, ok := m.data[key]
	if !ok {
		return mockEntry{}, false
	}
	if !e.exp.IsZero() && !m.now().Before(e.exp) {
		delete(m.data, key)
		return mockEntry{}, false
	}
	return e, true
}

// Get implements cache.Cache.
func (m *Mock) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("get"); err != nil {
		return nil, err
	}
	e, ok := m.live(key)
	if !ok {
		return nil, cache.ErrNotFound
	}
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, nil
}

// GetMulti implements cache.Cache.
func (m *Mock) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("getmulti"); err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(keys))
	for _, k := range keys {
		if e, ok := m.live(k); ok {
			b := make([]byte, len(e.val))
			copy(b, e.val)
			out[k] = b
		}
	}
	return out, nil
}

// Has implements cache.Cache.
func (m *Mock) Has(ctx context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("has"); err != nil {
		return false, err
	}
	_, ok := m.live(key)
	return ok, nil
}

// TTL implements cache.Cache.
func (m *Mock) TTL(ctx context.Context, key string) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("ttl"); err != nil {
		return 0, err
	}
	e, ok := m.live(key)
	if !ok {
		return 0, cache.ErrNotFound
	}
	if e.exp.IsZero() {
		return 0, nil
	}
	return e.exp.Sub(m.now()), nil
}

func (m *Mock) expAt(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return m.now().Add(ttl)
}

// Set implements cache.Cache.
func (m *Mock) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("set"); err != nil {
		return err
	}
	b := make([]byte, len(val))
	copy(b, val)
	m.data[key] = mockEntry{val: b, exp: m.expAt(ttl)}
	return nil
}

// SetMulti implements cache.Cache.
func (m *Mock) SetMulti(ctx context.Context, items map[string]cache.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("setmulti"); err != nil {
		return err
	}
	for k, it := range items {
		b := make([]byte, len(it.Value))
		copy(b, it.Value)
		m.data[k] = mockEntry{val: b, exp: m.expAt(it.TTL)}
	}
	return nil
}

// SetNX implements cache.Cache.
func (m *Mock) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("setnx"); err != nil {
		return false, err
	}
	if _, ok := m.live(key); ok {
		return false, nil
	}
	b := make([]byte, len(val))
	copy(b, val)
	m.data[key] = mockEntry{val: b, exp: m.expAt(ttl)}
	return true, nil
}

// Expire implements cache.Cache.
func (m *Mock) Expire(ctx context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("expire"); err != nil {
		return err
	}
	e, ok := m.live(key)
	if !ok {
		return cache.ErrNotFound
	}
	e.exp = m.expAt(ttl)
	m.data[key] = e
	return nil
}

// Touch extends a key by the default TTL.
func (m *Mock) Touch(ctx context.Context, key string) error {
	return m.Expire(ctx, key, time.Hour)
}

func (m *Mock) addInt(key string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("incr"); err != nil {
		return 0, err
	}
	var cur int64
	if e, ok := m.live(key); ok && len(e.val) == 8 {
		cur = int64(binary.BigEndian.Uint64(e.val))
	}
	cur += delta
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(cur))
	old := m.data[key]
	m.data[key] = mockEntry{val: b, exp: old.exp}
	return cur, nil
}

// Incr implements cache.Cache.
func (m *Mock) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return m.addInt(key, delta)
}

// Decr implements cache.Cache.
func (m *Mock) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return m.addInt(key, -delta)
}

// Del implements cache.Cache.
func (m *Mock) Del(ctx context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("del"); err != nil {
		return err
	}
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

// DeleteByPrefix implements cache.Cache.
func (m *Mock) DeleteByPrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("deletebyprefix"); err != nil {
		return err
	}
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			delete(m.data, k)
		}
	}
	return nil
}

// Flush implements cache.Cache.
func (m *Mock) Flush(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("flush"); err != nil {
		return err
	}
	m.data = map[string]mockEntry{}
	return nil
}

// Iterate implements cache.Cache.
func (m *Mock) Iterate(ctx context.Context, opts cache.IterateOpts) cache.Iterator {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.data {
		if _, ok := m.live(k); ok && strings.HasPrefix(k, opts.Prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return &mockIter{m: m, keys: keys, pos: -1}
}

type mockIter struct {
	m    *Mock
	keys []string
	pos  int
	k    string
	v    []byte
}

func (it *mockIter) Next() bool {
	it.pos++
	for it.pos < len(it.keys) {
		it.m.mu.Lock()
		e, ok := it.m.live(it.keys[it.pos])
		it.m.mu.Unlock()
		if ok {
			it.k = it.keys[it.pos]
			it.v = append([]byte(nil), e.val...)
			return true
		}
		it.pos++
	}
	return false
}

func (it *mockIter) Key() string   { return it.k }
func (it *mockIter) Value() []byte { return it.v }
func (it *mockIter) Err() error    { return nil }
func (it *mockIter) Close() error  { return nil }

// Ping implements cache.Cache.
func (m *Mock) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fail("ping")
}

// Close marks the mock closed; subsequent ops return cache.ErrClosed.
func (m *Mock) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// Stats reports the live entry count.
func (m *Mock) Stats() cache.Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cache.Stats{Entries: int64(len(m.data))}
}

var _ cache.Cache = (*Mock)(nil)
