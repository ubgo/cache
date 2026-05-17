package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ubgo/cache"
)

func TestInProcInvalidationFanout(t *testing.T) {
	bus := cache.NewInProcInvalidation()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	got := map[int][]string{}
	var ready sync.WaitGroup
	for i := 0; i < 3; i++ {
		ready.Add(1)
		go func(id int) {
			started := false
			_ = bus.Subscribe(ctx, func(k string) {
				if !started {
					started = true
				}
				mu.Lock()
				got[id] = append(got[id], k)
				mu.Unlock()
			})
		}(i)
	}
	// Give subscribers a moment to register.
	time.Sleep(50 * time.Millisecond)

	if err := bus.Publish(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		done := len(got) == 3 && len(got[0]) == 2 && len(got[1]) == 2 && len(got[2]) == 2
		mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	for id := 0; id < 3; id++ {
		if len(got[id]) != 2 || got[id][0] != "a" || got[id][1] != "b" {
			t.Fatalf("subscriber %d got %v, want [a b]", id, got[id])
		}
	}
}

func TestInProcSubscribeStopsOnContext(t *testing.T) {
	bus := cache.NewInProcInvalidation()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Subscribe(ctx, func(string) {}) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Subscribe should return ctx error on cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancel")
	}
}
