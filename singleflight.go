package cache

import "sync"

// flightReg maps a Cache instance to its single-flight group so concurrent
// Remember calls for the same key on the same cache load exactly once. Keyed
// by interface identity; adapters are pointer-backed so this is stable.
//
// Why keyed by Cache (not a global): two independent caches may legitimately
// load the same logical key concurrently — they are different stores, so
// deduping across them would be wrong. Per-instance groups keep dedup scoped
// to one backing store. The map grows by one entry per distinct Cache value
// ever passed to Remember and is never pruned; this is intentional and bounded
// in practice because applications create a small, fixed set of caches.
var (
	flightRegMu sync.Mutex
	flightReg   map[Cache]*flightGroup
)

// flightGroup deduplicates concurrent calls for the same key: only the first
// caller runs fn, the rest block and share its result. Zero-dependency
// reimplementation of the golang.org/x/sync/singleflight idea, scoped to what
// Remember needs.
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

// Do runs fn for key, or shares an in-flight call's result. The bool reports
// whether the result was shared (true) rather than computed by this caller.
//
// Concurrency contract: the map is guarded by g.mu, but fn runs OUTSIDE the
// lock so a slow loader never blocks unrelated keys. Followers find the
// in-flight *flightCall under the lock, release it, then block on the
// WaitGroup — they read c.val/c.err only after the leader's wg.Done(), so the
// fields are written-before-read with no data race. The leader deletes the
// entry after completing, so a fresh call for the same key after this one
// returns starts a new flight (no result caching here — that is Remember's
// job via the cache itself).
func (g *flightGroup) Do(key string, fn func() (any, error)) (v any, shared bool, err error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		// A leader is already running fn for this key. Drop the lock and wait
		// for it; share its result instead of running fn a second time.
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, true, c.err
	}
	// We are the leader for this key. Register before unlocking so any
	// follower that arrives while fn runs finds us.
	c := &flightCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done() // releases every follower currently blocked in Wait()

	// Deregister so the next call for this key starts fresh. Done under the
	// lock to stay consistent with the registration path above.
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.val, false, c.err
}
