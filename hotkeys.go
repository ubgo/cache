package cache

import (
	"context"
	"sort"
	"sync"
	"time"
)

// HotKeyTracker wraps a Cache and approximates the most frequently *read* keys
// using the Space-Saving algorithm: a fixed pool of counters (capacity), O(1)
// per access, bounded memory regardless of keyspace size. It surfaces the one
// key that is 60% of your traffic so you can fix a hotspot you otherwise
// cannot see.
//
//	hk := cache.NewHotKeyTracker(backend, 100)
//	c := hk // use as a normal cache.Cache
//	...
//	for _, kc := range hk.Top(10) { log.Printf("%s ~%d", kc.Key, kc.Est) }
//
// Counts are approximate (Space-Saving over-estimates by at most the evicted
// minimum); ranking of genuinely hot keys is reliable, exact counts are not.
type HotKeyTracker struct {
	Cache
	cap int

	mu     sync.Mutex
	counts map[string]int64 // tracked key -> estimated count
}

// NewHotKeyTracker wraps c, tracking at most capacity candidate keys.
func NewHotKeyTracker(c Cache, capacity int) *HotKeyTracker {
	if capacity < 1 {
		capacity = 1
	}
	return &HotKeyTracker{Cache: c, cap: capacity, counts: make(map[string]int64, capacity)}
}

func (h *HotKeyTracker) observe(key string) {
	h.mu.Lock()
	if _, ok := h.counts[key]; ok {
		h.counts[key]++
		h.mu.Unlock()
		return
	}
	if len(h.counts) < h.cap {
		h.counts[key] = 1
		h.mu.Unlock()
		return
	}
	// Pool full: evict the current minimum and inherit its count + 1
	// (Space-Saving). This bounds memory and lets a newly-hot key climb.
	var minK string
	var minV int64 = -1
	for k, v := range h.counts {
		if minV < 0 || v < minV {
			minK, minV = k, v
		}
	}
	delete(h.counts, minK)
	h.counts[key] = minV + 1
	h.mu.Unlock()
}

// KeyCount is one entry of a Top result.
type KeyCount struct {
	Key string
	Est int64 // estimated access count (upper bound)
}

// Top returns up to n tracked keys, hottest first.
func (h *HotKeyTracker) Top(n int) []KeyCount {
	h.mu.Lock()
	out := make([]KeyCount, 0, len(h.counts))
	for k, v := range h.counts {
		out = append(out, KeyCount{Key: k, Est: v})
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Est != out[j].Est {
			return out[i].Est > out[j].Est
		}
		return out[i].Key < out[j].Key
	})
	if n > 0 && n < len(out) {
		out = out[:n]
	}
	return out
}

// Reset clears all tracking (e.g. per sampling window).
func (h *HotKeyTracker) Reset() {
	h.mu.Lock()
	h.counts = make(map[string]int64, h.cap)
	h.mu.Unlock()
}

// Get records the access then delegates.
func (h *HotKeyTracker) Get(ctx context.Context, key string) ([]byte, error) {
	h.observe(key)
	return h.Cache.Get(ctx, key)
}

// GetMulti records each requested key then delegates.
func (h *HotKeyTracker) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	for _, k := range keys {
		h.observe(k)
	}
	return h.Cache.GetMulti(ctx, keys)
}

// TTL is not a data read; pass through without affecting hot-key stats.
func (h *HotKeyTracker) TTL(ctx context.Context, key string) (time.Duration, error) {
	return h.Cache.TTL(ctx, key)
}

var _ Cache = (*HotKeyTracker)(nil)
