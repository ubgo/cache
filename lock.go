// lock.go — Locker: a portable distributed lock built on SetNX (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares Locker + ErrLockNotAcquired and implements NewLock
// using only the Cache SetNX primitive, so the lock works on any adapter
// with no backend-specific code. The WHY: cross-process mutual exclusion
// (cron leader election, etc.) without Redis-specific Lua. Safety model /
// invariant: each Locker holds a random 16-byte token written as the lock
// value; Refresh/Release re-read and proceed only if the token still
// matches, so a holder whose lease expired cannot stomp the new owner.
//
// AI-context: this is check-then-act, NOT atomic compare-and-delete — it
// closes the common "released someone else's lock" race but is not a
// fencing token; keys are prefixed with the const lockPrefix "__lock__:".

package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// ErrLockNotAcquired is returned by Acquire when the lock is already held.
var ErrLockNotAcquired = errors.New("cache: lock not acquired")

// Locker is a portable distributed lock built on the Cache SetNX primitive.
// Works on any adapter without backend-specific code.
//
// Safety model: each Locker carries a random 16-byte token written as the
// lock value. Refresh/Release re-read the value and proceed only if it still
// matches this holder's token, so a holder whose lease already expired (and
// was re-acquired by someone else) cannot stomp the new owner. This is
// check-then-act, not atomic compare-and-delete: it closes the common
// "released someone else's lock" race but is not a substitute for a fencing
// token in systems that require strict mutual exclusion under arbitrary
// pauses. Use a short-enough TTL plus periodic Refresh for long critical
// sections.
type Locker interface {
	// Acquire creates the lock with the given TTL. Returns ErrLockNotAcquired
	// if already held by someone else.
	Acquire(ctx context.Context) error
	// Refresh extends the TTL iff this holder still owns the lock.
	Refresh(ctx context.Context) error
	// Release frees the lock iff this holder still owns it (token-checked, so a
	// caller cannot release a lock that already expired and was re-taken).
	Release(ctx context.Context) error
}

type lock struct {
	c     Cache
	key   string
	ttl   time.Duration
	token []byte
}

// NewLock returns a Locker for key with the given lease TTL.
//
//	l := cache.NewLock(redis, "cron:nightly-billing", 30*time.Second)
//	if err := l.Acquire(ctx); err != nil { return }  // another pod owns it
//	defer l.Release(ctx)
func NewLock(c Cache, key string, ttl time.Duration) Locker {
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	return &lock{c: c, key: lockPrefix + key, ttl: ttl, token: tok}
}

const lockPrefix = "__lock__:"

func (l *lock) Acquire(ctx context.Context) error {
	ok, err := l.c.SetNX(ctx, l.key, l.token, l.ttl)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLockNotAcquired
	}
	return nil
}

func (l *lock) owns(ctx context.Context) (bool, error) {
	cur, err := l.c.Get(ctx, l.key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return hex.EncodeToString(cur) == hex.EncodeToString(l.token), nil
}

func (l *lock) Refresh(ctx context.Context) error {
	owns, err := l.owns(ctx)
	if err != nil {
		return err
	}
	if !owns {
		return ErrLockNotAcquired
	}
	return l.c.Expire(ctx, l.key, l.ttl)
}

func (l *lock) Release(ctx context.Context) error {
	owns, err := l.owns(ctx)
	if err != nil {
		return err
	}
	if !owns {
		return nil // someone else's (or expired) — nothing to release
	}
	return l.c.Del(ctx, l.key)
}
