// audit.go — NewAuditLog: a mutation audit-trail Cache wrapper (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares AuditEvent/AuditFunc and implements NewAuditLog which
// wraps a Cache so every state-changing op (Set/SetMulti/SetNX/Expire/Touch/
// Incr/Decr/Del/DeleteByPrefix/Flush) emits an AuditEvent — a compliance
// trail of what was written or purged and when. The WHY: regulated
// environments need a tamper record of cache mutations. Privacy note /
// contrast with obs.go: audit events DELIBERATELY include RAW keys (that is
// their purpose) — route them to a TRUSTED sink, unlike the hashed obs hooks.
//
// AI-context: this is a Cache decorator; reads are intentionally NOT audited
// (only mutations). AuditFunc must not block — it runs inline on the op
// path. The `var _ Cache` assertion guards interface drift.

package cache

import (
	"context"
	"time"
)

// AuditEvent describes one completed state-changing operation. Keys carries
// the affected key(s) (or the prefix for DeleteByPrefix; empty for Flush).
// Audit logs intentionally include raw keys — that is their purpose — so route
// them to a trusted sink.
type AuditEvent struct {
	Op   string
	Keys []string
	Err  error
	At   time.Time
}

// AuditFunc receives every state-changing event. It must not block.
type AuditFunc func(AuditEvent)

type auditCache struct {
	Cache
	fn    AuditFunc
	clock func() time.Time
}

// NewAuditLog wraps c so every mutation (Set/SetMulti/SetNX/Expire/Touch/
// Incr/Decr/Del/DeleteByPrefix/Flush) emits an AuditEvent — a compliance trail
// of what was written or purged and when. Reads are not audited.
func NewAuditLog(c Cache, fn AuditFunc) Cache {
	return &auditCache{Cache: c, fn: fn, clock: time.Now}
}

func (a *auditCache) emit(op string, keys []string, err error) {
	if a.fn != nil {
		a.fn(AuditEvent{Op: op, Keys: keys, Err: err, At: a.clock()})
	}
}

func (a *auditCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	err := a.Cache.Set(ctx, key, val, ttl)
	a.emit("set", []string{key}, err)
	return err
}

func (a *auditCache) SetMulti(ctx context.Context, items map[string]Item) error {
	err := a.Cache.SetMulti(ctx, items)
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	a.emit("setmulti", keys, err)
	return err
}

func (a *auditCache) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	ok, err := a.Cache.SetNX(ctx, key, val, ttl)
	a.emit("setnx", []string{key}, err)
	return ok, err
}

func (a *auditCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	err := a.Cache.Expire(ctx, key, ttl)
	a.emit("expire", []string{key}, err)
	return err
}

func (a *auditCache) Touch(ctx context.Context, key string) error {
	err := a.Cache.Touch(ctx, key)
	a.emit("touch", []string{key}, err)
	return err
}

func (a *auditCache) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	v, err := a.Cache.Incr(ctx, key, delta)
	a.emit("incr", []string{key}, err)
	return v, err
}

func (a *auditCache) Decr(ctx context.Context, key string, delta int64) (int64, error) {
	v, err := a.Cache.Decr(ctx, key, delta)
	a.emit("decr", []string{key}, err)
	return v, err
}

func (a *auditCache) Del(ctx context.Context, keys ...string) error {
	err := a.Cache.Del(ctx, keys...)
	a.emit("del", keys, err)
	return err
}

func (a *auditCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	err := a.Cache.DeleteByPrefix(ctx, prefix)
	a.emit("deletebyprefix", []string{prefix}, err)
	return err
}

func (a *auditCache) Flush(ctx context.Context) error {
	err := a.Cache.Flush(ctx)
	a.emit("flush", nil, err)
	return err
}

var _ Cache = (*auditCache)(nil)
