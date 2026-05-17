// invalidation.go — cross-process "these keys changed" fan-out bus (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares the Invalidation interface (Publish/Subscribe), the
// InvalidateAll broadcast sentinel (empty string = "drop everything"), and
// NewInProcInvalidation — an in-process bus that is also the reference
// semantics every distributed implementation must match. The WHY: a
// tiered/L1 cache subscribes so it drops locally-held copies when another
// node mutates a key. Invariant: delivery is best-effort — a missed message
// only means a stale L1 entry until its own short TTL elapses, never
// correctness loss for a write-through path.
//
// AI-context: Subscribe BLOCKS until ctx is done — callers must run it in
// its own goroutine; the in-proc channel buffer is finite so a slow
// subscriber backpressures Publish (intentional, not a leak).

package cache

import (
	"context"
	"sync"
)

// Invalidation is a best-effort fan-out of "these keys changed" events across
// processes. A tiered/L1 cache subscribes so it can drop locally-cached copies
// when another node mutates a key. Delivery is best-effort: a missed message
// only means a stale L1 entry until its own (short) TTL elapses.
//
// The InvalidateAll sentinel (empty key) means "drop everything you hold".
type Invalidation interface {
	// Publish announces that keys were invalidated. Safe for concurrent use.
	Publish(ctx context.Context, keys ...string) error
	// Subscribe calls fn for every invalidated key until ctx is cancelled.
	// It blocks until ctx is done; run it in its own goroutine.
	Subscribe(ctx context.Context, fn func(key string)) error
}

// InvalidateAll is the broadcast sentinel: subscribers receiving it should
// drop their entire local view.
const InvalidateAll = ""

// inproc is an in-process Invalidation (single binary / tests). It is also the
// reference semantics every distributed implementation must match.
type inproc struct {
	mu   sync.Mutex
	subs map[int]chan string
	next int
}

// NewInProcInvalidation returns an in-process Invalidation bus.
func NewInProcInvalidation() Invalidation {
	return &inproc{subs: map[int]chan string{}}
}

func (b *inproc) Publish(ctx context.Context, keys ...string) error {
	b.mu.Lock()
	chans := make([]chan string, 0, len(b.subs))
	for _, ch := range b.subs {
		chans = append(chans, ch)
	}
	b.mu.Unlock()
	for _, k := range keys {
		for _, ch := range chans {
			select {
			case ch <- k:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (b *inproc) Subscribe(ctx context.Context, fn func(key string)) error {
	ch := make(chan string, 256)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case k := <-ch:
			fn(k)
		}
	}
}
