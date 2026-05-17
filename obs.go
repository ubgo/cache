// obs.go — Instrument: zero-dependency observability seam over a Cache (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares ObsHooks/OpEvent, the KeyHash helper, and Instrument
// which wraps a Cache so every op reports through callbacks and updates
// internal counters. The WHY: the core never imports OpenTelemetry or
// Prometheus — contrib/cache-otel and contrib/cache-prom implement the
// hooks. Privacy invariant: events carry KeyHash (first 8 bytes of
// sha256(key), hex) — raw keys may contain PII and are NEVER emitted here.
//
// AI-context: this is a Cache decorator. By design only Get/Set/Del emit
// OpEvents (dominant traffic + the hit/miss outcome that matters for SLOs);
// other ops pass straight through to keep the hot path cheap. Stats() MERGES
// the wrapper's observed counters on top of the adapter's snapshot (each
// layer counts only ops flowing through it, so no double counting).

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"
	"time"
)

// ObsHooks are zero-dependency observability seams. The core never imports
// OpenTelemetry or Prometheus; the contrib/cache-otel and contrib/cache-prom
// modules implement these callbacks. All fields are optional.
type ObsHooks struct {
	// Adapter / Namespace are attached to every event for labelling.
	Adapter   string
	Namespace string

	// OnOp fires after every operation with its outcome.
	OnOp func(ev OpEvent)
	// OnHit / OnMiss fire on read outcomes (KeyHash, never the raw key).
	OnHit  func(keyHash string)
	OnMiss func(keyHash string)
}

// OpEvent describes one completed cache operation. KeyHash is the first 8
// bytes of sha256(key) hex-encoded — raw keys may contain PII and are never
// emitted.
type OpEvent struct {
	Op        string
	Adapter   string
	Namespace string
	KeyHash   string
	Hit       bool
	Err       error
	Duration  time.Duration
}

// KeyHash returns the privacy-safe key identifier used in spans/metrics.
func KeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// Instrument wraps c so every operation reports through hooks and updates an
// internal Stats counter set (merged over whatever the adapter already
// reports). Observability is on by default in this library; pass a zero
// ObsHooks for counters-only with no exporter.
func Instrument(c Cache, hooks ObsHooks) Cache {
	return &obsCache{Cache: c, h: hooks}
}

type obsCache struct {
	Cache
	h      ObsHooks
	hits   atomic.Int64
	misses atomic.Int64
	sets   atomic.Int64
	dels   atomic.Int64
}

func (o *obsCache) record(op, key string, hit bool, err error, start time.Time) {
	if o.h.OnOp != nil {
		o.h.OnOp(OpEvent{
			Op: op, Adapter: o.h.Adapter, Namespace: o.h.Namespace,
			KeyHash: KeyHash(key), Hit: hit, Err: err,
			Duration: time.Since(start),
		})
	}
}

func (o *obsCache) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	v, err := o.Cache.Get(ctx, key)
	hit := err == nil
	if hit {
		o.hits.Add(1)
		if o.h.OnHit != nil {
			o.h.OnHit(KeyHash(key))
		}
	} else {
		o.misses.Add(1)
		if o.h.OnMiss != nil {
			o.h.OnMiss(KeyHash(key))
		}
	}
	o.record("get", key, hit, err, start)
	return v, err
}

func (o *obsCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	start := time.Now()
	err := o.Cache.Set(ctx, key, val, ttl)
	o.sets.Add(1)
	o.record("set", key, false, err, start)
	return err
}

func (o *obsCache) Del(ctx context.Context, keys ...string) error {
	start := time.Now()
	err := o.Cache.Del(ctx, keys...)
	o.dels.Add(int64(len(keys)))
	k := ""
	if len(keys) > 0 {
		k = keys[0]
	}
	o.record("del", k, false, err, start)
	return err
}

// Only Get/Set/Del are instrumented (the dominant traffic and the ones whose
// hit/miss outcome matters for SLOs). Other ops (Has/TTL/Incr/...) pass
// straight through the embedded Cache without an OpEvent — by design, to keep
// the hot path cheap. If you need every op traced, wrap at the adapter level
// or use the contrib exporters.

// Stats merges the wrapper's observed counters ON TOP of the adapter's
// snapshot rather than replacing it: the adapter may track evictions, byte
// counts, or hits the wrapper never sees (e.g. served by an inner tier), and
// the wrapper adds the hits/misses/sets/deletes it observed at this layer.
// Double-counting is avoided because each layer counts only the ops that flow
// through it.
func (o *obsCache) Stats() Stats {
	s := o.Cache.Stats()
	s.Hits += o.hits.Load()
	s.Misses += o.misses.Load()
	s.Sets += o.sets.Load()
	s.Deletes += o.dels.Load()
	return s
}
