package cache_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
)

func TestAuditLogEmitsMutations(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var events []cache.AuditEvent
	c := cache.NewAuditLog(cachetest.NewMock(), func(e cache.AuditEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_ = c.Set(ctx, "k", []byte("v"), time.Minute)
	_, _ = c.Get(ctx, "k") // read: must NOT audit
	_ = c.Del(ctx, "k")
	_ = c.Flush(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("want 3 audited mutations (set,del,flush), got %d: %+v", len(events), events)
	}
	if events[0].Op != "set" || events[0].Keys[0] != "k" {
		t.Fatalf("set event wrong: %+v", events[0])
	}
	if events[1].Op != "del" || events[1].Keys[0] != "k" {
		t.Fatalf("del event wrong: %+v", events[1])
	}
	if events[2].Op != "flush" || len(events[2].Keys) != 0 {
		t.Fatalf("flush event wrong: %+v", events[2])
	}
	if events[0].At.IsZero() {
		t.Fatal("event timestamp not set")
	}
}

func TestAuditLogConforms(t *testing.T) {
	cachetest.Run(t, func(_ *testing.T) cache.Cache {
		return cache.NewAuditLog(cachetest.NewMock(), func(cache.AuditEvent) {})
	})
}

func TestBulkheadCapsConcurrency(t *testing.T) {
	ctx := context.Background()
	const limit = 4
	gate := make(chan struct{})
	var inFlight, maxSeen int64

	// blocking backend: every Get parks until gate is closed.
	blk := &blockingCache{Mock: cachetest.NewMock(), gate: gate,
		onEnter: func() {
			n := atomic.AddInt64(&inFlight, 1)
			for {
				m := atomic.LoadInt64(&maxSeen)
				if n <= m || atomic.CompareAndSwapInt64(&maxSeen, m, n) {
					break
				}
			}
		},
		onExit: func() { atomic.AddInt64(&inFlight, -1) },
	}
	c := cache.NewBulkhead(blk, limit)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.Get(ctx, "k") }()
	}
	time.Sleep(100 * time.Millisecond) // let goroutines pile up against the cap
	close(gate)
	wg.Wait()

	if got := atomic.LoadInt64(&maxSeen); got > limit {
		t.Fatalf("bulkhead breached: %d concurrent > limit %d", got, limit)
	}
}

func TestBulkheadRespectsContext(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	blk := &blockingCache{Mock: cachetest.NewMock(), gate: gate}
	c := cache.NewBulkhead(blk, 1)

	// Occupy the only slot.
	go func() { _, _ = c.Get(context.Background(), "x") }()
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, "y")
	if err == nil {
		t.Fatal("expected context error when bulkhead is saturated")
	}
}

func TestBulkheadConforms(t *testing.T) {
	cachetest.Run(t, func(_ *testing.T) cache.Cache {
		return cache.NewBulkhead(cachetest.NewMock(), 8)
	})
}

// blockingCache parks Get until gate is closed; hooks observe concurrency.
type blockingCache struct {
	*cachetest.Mock
	gate    chan struct{}
	onEnter func()
	onExit  func()
}

func (b *blockingCache) Get(ctx context.Context, key string) ([]byte, error) {
	if b.onEnter != nil {
		b.onEnter()
	}
	<-b.gate
	if b.onExit != nil {
		b.onExit()
	}
	return b.Mock.Get(ctx, key)
}
