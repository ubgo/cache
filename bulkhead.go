package cache

import (
	"context"
	"time"
)

// bulkhead bounds the number of in-flight operations so one overloaded
// caller/namespace cannot exhaust backend connections and starve everyone
// else. Acquire is context-aware: a slow backend surfaces as ctx.Err() (or
// ErrTimeout) rather than unbounded goroutine growth.
type bulkhead struct {
	Cache
	sem chan struct{}
}

// NewBulkhead wraps c, allowing at most maxConcurrent operations to be in
// flight at once; further ops block until a slot frees or their context is
// done. maxConcurrent < 1 is treated as 1.
func NewBulkhead(c Cache, maxConcurrent int) Cache {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &bulkhead{Cache: c, sem: make(chan struct{}, maxConcurrent)}
}

func (b *bulkhead) acquire(ctx context.Context) error {
	select {
	case b.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrTimeout
	}
}

func (b *bulkhead) release() { <-b.sem }

func bulkheadGuard[T any](ctx context.Context, b *bulkhead, fn func() (T, error)) (T, error) {
	var zero T
	if err := b.acquire(ctx); err != nil {
		return zero, err
	}
	defer b.release()
	return fn()
}

func bulkheadDo(ctx context.Context, b *bulkhead, fn func() error) error {
	if err := b.acquire(ctx); err != nil {
		return err
	}
	defer b.release()
	return fn()
}

func (b *bulkhead) Get(ctx context.Context, key string) ([]byte, error) {
	return bulkheadGuard(ctx, b, func() ([]byte, error) { return b.Cache.Get(ctx, key) })
}

func (b *bulkhead) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	return bulkheadGuard(ctx, b, func() (map[string][]byte, error) { return b.Cache.GetMulti(ctx, keys) })
}

func (b *bulkhead) Has(ctx context.Context, key string) (bool, error) {
	return bulkheadGuard(ctx, b, func() (bool, error) { return b.Cache.Has(ctx, key) })
}

func (b *bulkhead) TTL(ctx context.Context, key string) (time.Duration, error) {
	return bulkheadGuard(ctx, b, func() (time.Duration, error) { return b.Cache.TTL(ctx, key) })
}

func (b *bulkhead) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Set(ctx, key, val, ttl) })
}

func (b *bulkhead) SetMulti(ctx context.Context, items map[string]Item) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.SetMulti(ctx, items) })
}

func (b *bulkhead) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return bulkheadGuard(ctx, b, func() (bool, error) { return b.Cache.SetNX(ctx, key, val, ttl) })
}

func (b *bulkhead) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Expire(ctx, key, ttl) })
}

func (b *bulkhead) Touch(ctx context.Context, key string) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Touch(ctx, key) })
}

func (b *bulkhead) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return bulkheadGuard(ctx, b, func() (int64, error) { return b.Cache.Incr(ctx, key, delta) })
}

func (b *bulkhead) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return bulkheadGuard(ctx, b, func() (int64, error) { return b.Cache.Decr(ctx, key, delta) })
}

func (b *bulkhead) Del(ctx context.Context, keys ...string) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Del(ctx, keys...) })
}

func (b *bulkhead) DeleteByPrefix(ctx context.Context, prefix string) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.DeleteByPrefix(ctx, prefix) })
}

func (b *bulkhead) Flush(ctx context.Context) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Flush(ctx) })
}

func (b *bulkhead) Ping(ctx context.Context) error {
	return bulkheadDo(ctx, b, func() error { return b.Cache.Ping(ctx) })
}

var _ Cache = (*bulkhead)(nil)
