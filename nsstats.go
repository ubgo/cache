// nsstats.go — NamespaceStats: per-namespace hit/miss/set/delete counters (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements NamespaceStats (a Cache wrapper), the NamespaceFn
// bucketing strategy and DefaultNamespaceFn (split on the first ":"), plus
// ByNamespace() snapshots. The WHY: a great global hit-rate can hide one
// broken feature — bucketing per namespace makes the regression visible.
// Invariant: bucketing adds one map lookup per op; only Get/GetMulti/Set/Del
// are counted; a Get error counts as a miss only when it is ErrNotFound
// (real backend errors are neither hit nor miss).
//
// AI-context: this is a Cache decorator distinct from obs.go — obs is for
// exporters/SLOs, nsstats is for per-feature breakdown. The compile-time
// `var _ Cache` assertion guards interface drift.

package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// NamespaceFn derives a metrics bucket from a key. The default splits on the
// first ":" (so "user:42" and "user:99" bucket as "user"); keys without a
// separator bucket as "" (overall).
type NamespaceFn func(key string) string

// DefaultNamespaceFn buckets by the prefix before the first ":".
func DefaultNamespaceFn(key string) string {
	if i := strings.IndexByte(key, ':'); i >= 0 {
		return key[:i]
	}
	return ""
}

// NamespaceStats wraps a Cache and records hit/miss/set/delete counters per
// namespace, so a great global hit-rate can't hide one broken feature. It is
// itself a Cache; bucketing adds one map lookup per op.
//
//	ns := cache.NewNamespaceStats(backend, nil) // default prefix-before-colon
//	c := ns
//	...
//	for name, s := range ns.ByNamespace() {
//	    log.Printf("%s hit-rate %.2f", name, s.HitRatio())
//	}
type NamespaceStats struct {
	Cache
	nsFn NamespaceFn

	mu sync.Mutex
	by map[string]*Stats
}

// NewNamespaceStats wraps c. nsFn may be nil (uses DefaultNamespaceFn).
func NewNamespaceStats(c Cache, nsFn NamespaceFn) *NamespaceStats {
	if nsFn == nil {
		nsFn = DefaultNamespaceFn
	}
	return &NamespaceStats{Cache: c, nsFn: nsFn, by: map[string]*Stats{}}
}

func (n *NamespaceStats) bucket(key string) *Stats {
	name := n.nsFn(key)
	n.mu.Lock()
	s, ok := n.by[name]
	if !ok {
		s = &Stats{}
		n.by[name] = s
	}
	n.mu.Unlock()
	return s
}

// ByNamespace returns a snapshot copy of per-namespace counters.
func (n *NamespaceStats) ByNamespace() map[string]Stats {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]Stats, len(n.by))
	for k, v := range n.by {
		out[k] = *v
	}
	return out
}

// Get records hit/miss for the key's namespace then delegates.
func (n *NamespaceStats) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := n.Cache.Get(ctx, key)
	s := n.bucket(key)
	n.mu.Lock()
	if err == nil {
		s.Hits++
	} else if errors.Is(err, ErrNotFound) {
		s.Misses++
	}
	n.mu.Unlock()
	return v, err
}

// GetMulti records per-namespace hit/miss for each requested key.
func (n *NamespaceStats) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	res, err := n.Cache.GetMulti(ctx, keys)
	if err != nil {
		return res, err
	}
	for _, k := range keys {
		s := n.bucket(k)
		n.mu.Lock()
		if _, ok := res[k]; ok {
			s.Hits++
		} else {
			s.Misses++
		}
		n.mu.Unlock()
	}
	return res, nil
}

// Set records a set for the key's namespace then delegates.
func (n *NamespaceStats) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	err := n.Cache.Set(ctx, key, val, ttl)
	if err == nil {
		s := n.bucket(key)
		n.mu.Lock()
		s.Sets++
		n.mu.Unlock()
	}
	return err
}

// Del records deletes per namespace then delegates.
func (n *NamespaceStats) Del(ctx context.Context, keys ...string) error {
	err := n.Cache.Del(ctx, keys...)
	if err == nil {
		for _, k := range keys {
			s := n.bucket(k)
			n.mu.Lock()
			s.Deletes++
			n.mu.Unlock()
		}
	}
	return err
}

var _ Cache = (*NamespaceStats)(nil)
