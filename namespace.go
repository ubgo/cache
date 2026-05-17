// namespace.go — Namespaced: a key-prefixing Cache view over a shared backend (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements Namespaced(c, prefix) returning an nsCache that
// transparently prefixes every key (a trailing ":" is appended if absent)
// so multiple services share one backend without collisions, plus nsIter
// which strips the prefix back off on iteration. The WHY + key invariant:
// Flush on a namespaced view scopes to the prefix via DeleteByPrefix rather
// than wiping the whole backend (an empty prefix falls back to real Flush).
//
// AI-context: this is a Cache decorator (embeds Cache, overrides every key-
// carrying method to prefix in / strip out). Prefixing must stay symmetric
// — anything that adds the prefix on write must remove it on read-back.

package cache

import (
	"context"
	"strings"
	"time"
)

// Namespaced returns a Cache view that transparently prefixes every key with
// prefix (a trailing ":" is added if absent). Multiple services can share one
// backend without key collisions:
//
//	billing := cache.Namespaced(redis, "svc:billing")
//	search  := cache.Namespaced(redis, "svc:search")
//
// Flush on a namespaced view scopes to the prefix via DeleteByPrefix rather
// than wiping the whole backend.
func Namespaced(c Cache, prefix string) Cache {
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &nsCache{Cache: c, prefix: prefix}
}

type nsCache struct {
	Cache
	prefix string
}

func (n *nsCache) k(key string) string  { return n.prefix + key }
func (n *nsCache) uk(key string) string { return strings.TrimPrefix(key, n.prefix) }

func (n *nsCache) Get(ctx context.Context, key string) ([]byte, error) {
	return n.Cache.Get(ctx, n.k(key))
}

func (n *nsCache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	pk := make([]string, len(keys))
	for i, key := range keys {
		pk[i] = n.k(key)
	}
	raw, err := n.Cache.GetMulti(ctx, pk)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(raw))
	for k, v := range raw {
		out[n.uk(k)] = v
	}
	return out, nil
}

func (n *nsCache) Has(ctx context.Context, key string) (bool, error) {
	return n.Cache.Has(ctx, n.k(key))
}

func (n *nsCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	return n.Cache.TTL(ctx, n.k(key))
}

func (n *nsCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return n.Cache.Set(ctx, n.k(key), val, ttl)
}

func (n *nsCache) SetMulti(ctx context.Context, items map[string]Item) error {
	pi := make(map[string]Item, len(items))
	for k, v := range items {
		pi[n.k(k)] = v
	}
	return n.Cache.SetMulti(ctx, pi)
}

func (n *nsCache) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return n.Cache.SetNX(ctx, n.k(key), val, ttl)
}

func (n *nsCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return n.Cache.Expire(ctx, n.k(key), ttl)
}

func (n *nsCache) Touch(ctx context.Context, key string) error {
	return n.Cache.Touch(ctx, n.k(key))
}

func (n *nsCache) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return n.Cache.Incr(ctx, n.k(key), delta)
}

func (n *nsCache) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return n.Cache.Decr(ctx, n.k(key), delta)
}

func (n *nsCache) Del(ctx context.Context, keys ...string) error {
	pk := make([]string, len(keys))
	for i, key := range keys {
		pk[i] = n.k(key)
	}
	return n.Cache.Del(ctx, pk...)
}

func (n *nsCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	return n.Cache.DeleteByPrefix(ctx, n.k(prefix))
}

// Flush scopes to this namespace instead of wiping the whole backend.
func (n *nsCache) Flush(ctx context.Context) error {
	if n.prefix == "" {
		return n.Cache.Flush(ctx)
	}
	return n.Cache.DeleteByPrefix(ctx, n.prefix)
}

func (n *nsCache) Iterate(ctx context.Context, opts IterateOpts) Iterator {
	opts.Prefix = n.k(opts.Prefix)
	return &nsIter{Iterator: n.Cache.Iterate(ctx, opts), prefix: n.prefix}
}

type nsIter struct {
	Iterator
	prefix string
}

func (i *nsIter) Key() string { return strings.TrimPrefix(i.Iterator.Key(), i.prefix) }
