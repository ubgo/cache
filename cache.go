package cache

import (
	"context"
	"time"
)

// Cache is the bytes-level contract every backend implements. It is
// intentionally untyped; typed ergonomics live in the generics layer
// (GetT/SetT/Remember/Typed) so a single interface covers every adapter.
//
// Semantics every adapter MUST honor (enforced by cachetest.Run):
//
//   - Get returns (nil, ErrNotFound) on miss or expiry, never (nil, nil).
//   - A ttl <= 0 means "no expiry" (lives until evicted / explicitly deleted).
//   - SetNX returns (true, nil) only when it created the key.
//   - Incr/Decr are atomic; a missing key is treated as 0.
//   - Optional ops an adapter cannot serve return ErrUnsupported.
type Cache interface {
	// Read.
	Get(ctx context.Context, key string) ([]byte, error)
	GetMulti(ctx context.Context, keys []string) (map[string][]byte, error)
	Has(ctx context.Context, key string) (bool, error)
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Write.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetMulti(ctx context.Context, items map[string]Item) error
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Touch(ctx context.Context, key string) error

	// Atomic counters. delta may be negative; a missing key starts at 0.
	Incr(ctx context.Context, key string, delta int64) (int64, error)
	Decr(ctx context.Context, key string, delta int64) (int64, error)

	// Delete.
	Del(ctx context.Context, keys ...string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
	Flush(ctx context.Context) error

	// Iterate scans keys. Adapters that cannot scan return an Iterator whose
	// first Next() is false and Err() is ErrUnsupported.
	Iterate(ctx context.Context, opts IterateOpts) Iterator

	// Lifecycle.
	Ping(ctx context.Context) error
	Close() error

	// Stats returns a point-in-time snapshot. Adapters that do not track a
	// field leave it at its zero value.
	Stats() Stats
}

// Item is a value plus its per-entry TTL and optional tags, used by SetMulti.
type Item struct {
	Value []byte
	TTL   time.Duration
	Tags  []string // optional; adapters without tag support ignore it
}

// IterateOpts controls Iterate. Prefix "" iterates everything the adapter can
// see. Count is a backend hint for batch size (0 = adapter default).
type IterateOpts struct {
	Prefix string
	Count  int
}

// Iterator is a forward-only cursor. Always Close it.
//
//	it := c.Iterate(ctx, cache.IterateOpts{Prefix: "user:"})
//	defer it.Close()
//	for it.Next() {
//	    k, v := it.Key(), it.Value()
//	}
//	if err := it.Err(); err != nil { ... }
type Iterator interface {
	Next() bool
	Key() string
	Value() []byte
	Err() error
	Close() error
}
