// resilience_test.go — tests for the circuit breaker and retry wrappers (resilience.go).

package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

var errBackend = errors.New("backend down")

// flaky wraps a Mock; the first failGets Get calls fail with errBackend.
type flaky struct {
	*cachetest.Mock
	failGets int
	getCalls int
}

func (f *flaky) Get(ctx context.Context, key string) ([]byte, error) {
	f.getCalls++
	if f.failGets > 0 {
		f.failGets--
		return nil, errBackend
	}
	return f.Mock.Get(ctx, key)
}

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	ctx := context.Background()
	base := cachetest.NewMock()
	_ = base.Set(ctx, "k", []byte("v"), time.Minute)
	fk := &flaky{Mock: base, failGets: 2}

	r := cache.NewRetry(fk, cache.WithRetryAttempts(3), cache.WithRetryBackoff(time.Microsecond))
	v, err := r.Get(ctx, "k")
	if err != nil || string(v) != "v" {
		t.Fatalf("expected success after retries, got %q %v", v, err)
	}
	if fk.getCalls != 3 {
		t.Fatalf("expected 3 attempts, got %d", fk.getCalls)
	}
}

func TestRetryDoesNotRetryMiss(t *testing.T) {
	ctx := context.Background()
	fk := &flaky{Mock: cachetest.NewMock()}
	r := cache.NewRetry(fk, cache.WithRetryAttempts(5), cache.WithRetryBackoff(time.Microsecond))
	if _, err := r.Get(ctx, "absent"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if fk.getCalls != 1 {
		t.Fatalf("miss must not be retried; got %d attempts", fk.getCalls)
	}
}

func TestRetryGivesUpAfterAttempts(t *testing.T) {
	ctx := context.Background()
	fk := &flaky{Mock: cachetest.NewMock(), failGets: 99}
	r := cache.NewRetry(fk, cache.WithRetryAttempts(3), cache.WithRetryBackoff(time.Microsecond))
	if _, err := r.Get(ctx, "k"); !errors.Is(err, errBackend) {
		t.Fatalf("want errBackend after exhausting attempts, got %v", err)
	}
	if fk.getCalls != 3 {
		t.Fatalf("want exactly 3 attempts, got %d", fk.getCalls)
	}
}

func TestCircuitBreakerTripsAndRecovers(t *testing.T) {
	ctx := context.Background()
	base := cachetest.NewMock()
	_ = base.Set(ctx, "k", []byte("v"), time.Minute)
	fk := &flaky{Mock: base, failGets: 100}

	cb := cache.NewCircuitBreaker(fk,
		cache.WithBreakerThreshold(3),
		cache.WithBreakerCooldown(60*time.Millisecond))

	// 3 real failures trip the breaker.
	for i := 0; i < 3; i++ {
		if _, err := cb.Get(ctx, "k"); !errors.Is(err, errBackend) {
			t.Fatalf("attempt %d: want errBackend, got %v", i, err)
		}
	}
	callsBeforeOpen := fk.getCalls
	// Now open: fails fast without touching the backend.
	if _, err := cb.Get(ctx, "k"); !errors.Is(err, cache.ErrCircuitOpen) {
		t.Fatalf("want ErrCircuitOpen while open, got %v", err)
	}
	if fk.getCalls != callsBeforeOpen {
		t.Fatal("open circuit must not call the backend")
	}

	// Heal the backend, wait out the cooldown → half-open trial closes it.
	fk.failGets = 0
	time.Sleep(80 * time.Millisecond)
	v, err := cb.Get(ctx, "k")
	if err != nil || string(v) != "v" {
		t.Fatalf("expected recovery after cooldown, got %q %v", v, err)
	}
	if _, err := cb.Get(ctx, "k"); err != nil {
		t.Fatalf("circuit should be closed again, got %v", err)
	}
}

func TestResilienceDecoratorsConform(t *testing.T) {
	t.Run("breaker", func(t *testing.T) {
		cachetest.Run(t, func(_ *testing.T) cache.Cache {
			return cache.NewCircuitBreaker(cachetest.NewMock())
		})
	})
	t.Run("retry", func(t *testing.T) {
		cachetest.Run(t, func(_ *testing.T) cache.Cache {
			return cache.NewRetry(cachetest.NewMock(), cache.WithRetryBackoff(time.Microsecond))
		})
	})
}
