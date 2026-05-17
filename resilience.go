// resilience.go — circuit breaker + retry-with-backoff Cache wrappers (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements NewCircuitBreaker (opens after N consecutive
// failures, fails fast with ErrCircuitOpen, half-open trial after cooldown)
// and NewRetry (exponential backoff, ctx-abortable), plus the shared
// isFailure classifier. The WHY: a failing backend should stop absorbing
// doomed requests and transient blips should self-heal. Invariant:
// isFailure treats ErrNotFound / ErrUnsupported / ErrCircuitOpen as NORMAL
// outcomes — they never trip the breaker or trigger a retry; only genuine
// backend errors do.
//
// AI-context: both are Cache decorators that wrap every method through
// breakerGuard/retryGuard helpers; the `var _ Cache` assertions guard
// interface drift. Stacking order matters operationally (retry inside
// breaker vs outside) but is the caller's composition choice, not enforced.

package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

// isFailure reports whether err should count against resilience policies.
// A miss (ErrNotFound) and an unsupported optional op are normal outcomes,
// not backend failures, so they never trip a breaker or trigger a retry.
func isFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) ||
		errors.Is(err, ErrCircuitOpen) {
		return false
	}
	return true
}

// ---------- Circuit breaker ----------

// BreakerOption configures NewCircuitBreaker.
type BreakerOption func(*breaker)

// WithBreakerThreshold sets consecutive failures before the circuit opens
// (default 5).
func WithBreakerThreshold(n int) BreakerOption {
	return func(b *breaker) { b.threshold = n }
}

// WithBreakerCooldown sets how long the circuit stays open before a half-open
// trial request is allowed (default 5s).
func WithBreakerCooldown(d time.Duration) BreakerOption {
	return func(b *breaker) { b.cooldown = d }
}

type breaker struct {
	Cache
	threshold int
	cooldown  time.Duration
	clock     func() time.Time

	mu       sync.Mutex
	fails    int
	openedAt time.Time
	half     bool
}

// NewCircuitBreaker wraps c so a failing backend stops absorbing doomed
// requests: after threshold consecutive failures the circuit opens and ops
// fail fast with ErrCircuitOpen until a cooldown elapses, then one trial
// request decides whether to close again.
func NewCircuitBreaker(c Cache, opts ...BreakerOption) Cache {
	b := &breaker{Cache: c, threshold: 5, cooldown: 5 * time.Second, clock: time.Now}
	for _, o := range opts {
		o(b)
	}
	return b
}

// allow reports whether a request may proceed right now.
func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return true // closed
	}
	if b.clock().Sub(b.openedAt) >= b.cooldown {
		b.half = true // permit a single trial
		return true
	}
	return false
}

func (b *breaker) record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if isFailure(err) {
		b.fails++
		if b.half || b.fails >= b.threshold {
			b.openedAt = b.clock()
			b.half = false
		}
		return
	}
	// Success (or benign miss): reset.
	b.fails = 0
	b.openedAt = time.Time{}
	b.half = false
}

func breakerGuard[T any](b *breaker, fn func() (T, error)) (T, error) {
	var zero T
	if !b.allow() {
		return zero, ErrCircuitOpen
	}
	v, err := fn()
	b.record(err)
	return v, err
}

func breakerDo(b *breaker, fn func() error) error {
	_, err := breakerGuard(b, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

func (b *breaker) Get(ctx context.Context, key string) ([]byte, error) {
	return breakerGuard(b, func() ([]byte, error) { return b.Cache.Get(ctx, key) })
}

func (b *breaker) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	return breakerGuard(b, func() (map[string][]byte, error) { return b.Cache.GetMulti(ctx, keys) })
}

func (b *breaker) Has(ctx context.Context, key string) (bool, error) {
	return breakerGuard(b, func() (bool, error) { return b.Cache.Has(ctx, key) })
}

func (b *breaker) TTL(ctx context.Context, key string) (time.Duration, error) {
	return breakerGuard(b, func() (time.Duration, error) { return b.Cache.TTL(ctx, key) })
}

func (b *breaker) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return breakerDo(b, func() error { return b.Cache.Set(ctx, key, val, ttl) })
}

func (b *breaker) SetMulti(ctx context.Context, items map[string]Item) error {
	return breakerDo(b, func() error { return b.Cache.SetMulti(ctx, items) })
}

func (b *breaker) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return breakerGuard(b, func() (bool, error) { return b.Cache.SetNX(ctx, key, val, ttl) })
}

func (b *breaker) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return breakerDo(b, func() error { return b.Cache.Expire(ctx, key, ttl) })
}

func (b *breaker) Touch(ctx context.Context, key string) error {
	return breakerDo(b, func() error { return b.Cache.Touch(ctx, key) })
}

func (b *breaker) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return breakerGuard(b, func() (int64, error) { return b.Cache.Incr(ctx, key, delta) })
}

func (b *breaker) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return breakerGuard(b, func() (int64, error) { return b.Cache.Decr(ctx, key, delta) })
}

func (b *breaker) Del(ctx context.Context, keys ...string) error {
	return breakerDo(b, func() error { return b.Cache.Del(ctx, keys...) })
}

func (b *breaker) DeleteByPrefix(ctx context.Context, prefix string) error {
	return breakerDo(b, func() error { return b.Cache.DeleteByPrefix(ctx, prefix) })
}

func (b *breaker) Flush(ctx context.Context) error {
	return breakerDo(b, func() error { return b.Cache.Flush(ctx) })
}

func (b *breaker) Ping(ctx context.Context) error {
	return breakerDo(b, func() error { return b.Cache.Ping(ctx) })
}

var _ Cache = (*breaker)(nil)

// ---------- Retry with backoff ----------

// RetryOption configures NewRetry.
type RetryOption func(*retrier)

// WithRetryAttempts sets the maximum total attempts (default 3).
func WithRetryAttempts(n int) RetryOption {
	return func(r *retrier) { r.attempts = n }
}

// WithRetryBackoff sets the base delay; attempt i waits base * 2^(i-1).
func WithRetryBackoff(base time.Duration) RetryOption {
	return func(r *retrier) { r.base = base }
}

type retrier struct {
	Cache
	attempts int
	base     time.Duration
	sleep    func(time.Duration)
}

// NewRetry wraps c so transient backend errors are retried with exponential
// backoff. ErrNotFound / ErrUnsupported are returned immediately (not
// transient). Context cancellation aborts the retry loop.
func NewRetry(c Cache, opts ...RetryOption) Cache {
	r := &retrier{Cache: c, attempts: 3, base: 20 * time.Millisecond, sleep: time.Sleep}
	for _, o := range opts {
		o(r)
	}
	if r.attempts < 1 {
		r.attempts = 1
	}
	return r
}

func retryGuard[T any](ctx context.Context, r *retrier, fn func() (T, error)) (T, error) {
	var v T
	var err error
	for attempt := 1; attempt <= r.attempts; attempt++ {
		v, err = fn()
		if !isFailure(err) {
			return v, err
		}
		if attempt == r.attempts {
			break
		}
		delay := r.base << (attempt - 1)
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		default:
			r.sleep(delay)
		}
	}
	return v, err
}

func retryDo(ctx context.Context, r *retrier, fn func() error) error {
	_, err := retryGuard(ctx, r, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

func (r *retrier) Get(ctx context.Context, key string) ([]byte, error) {
	return retryGuard(ctx, r, func() ([]byte, error) { return r.Cache.Get(ctx, key) })
}

func (r *retrier) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	return retryGuard(ctx, r, func() (map[string][]byte, error) { return r.Cache.GetMulti(ctx, keys) })
}

func (r *retrier) Has(ctx context.Context, key string) (bool, error) {
	return retryGuard(ctx, r, func() (bool, error) { return r.Cache.Has(ctx, key) })
}

func (r *retrier) TTL(ctx context.Context, key string) (time.Duration, error) {
	return retryGuard(ctx, r, func() (time.Duration, error) { return r.Cache.TTL(ctx, key) })
}

func (r *retrier) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return retryDo(ctx, r, func() error { return r.Cache.Set(ctx, key, val, ttl) })
}

func (r *retrier) SetMulti(ctx context.Context, items map[string]Item) error {
	return retryDo(ctx, r, func() error { return r.Cache.SetMulti(ctx, items) })
}

func (r *retrier) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return retryGuard(ctx, r, func() (bool, error) { return r.Cache.SetNX(ctx, key, val, ttl) })
}

func (r *retrier) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return retryDo(ctx, r, func() error { return r.Cache.Expire(ctx, key, ttl) })
}

func (r *retrier) Touch(ctx context.Context, key string) error {
	return retryDo(ctx, r, func() error { return r.Cache.Touch(ctx, key) })
}

func (r *retrier) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return retryGuard(ctx, r, func() (int64, error) { return r.Cache.Incr(ctx, key, delta) })
}

func (r *retrier) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	return retryGuard(ctx, r, func() (int64, error) { return r.Cache.Decr(ctx, key, delta) })
}

func (r *retrier) Del(ctx context.Context, keys ...string) error {
	return retryDo(ctx, r, func() error { return r.Cache.Del(ctx, keys...) })
}

func (r *retrier) DeleteByPrefix(ctx context.Context, prefix string) error {
	return retryDo(ctx, r, func() error { return r.Cache.DeleteByPrefix(ctx, prefix) })
}

func (r *retrier) Flush(ctx context.Context) error {
	return retryDo(ctx, r, func() error { return r.Cache.Flush(ctx) })
}

func (r *retrier) Ping(ctx context.Context) error {
	return retryDo(ctx, r, func() error { return r.Cache.Ping(ctx) })
}

var _ Cache = (*retrier)(nil)
