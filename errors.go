// errors.go — the cross-backend sentinel error set (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: declares every sentinel error (ErrNotFound, ErrUnsupported,
// ErrSerialization, ErrTimeout, ErrCircuitOpen, ErrTooLarge, ErrKeyTooLong,
// ErrClosed). The WHY: adapters in sibling modules must return THESE values
// (wrapping with %w is allowed) so callers can errors.Is() against them
// regardless of which backend is wired in. The invariant ErrNotFound on a
// miss (never (nil, nil)) is the single most relied-on contract here.
//
// AI-context: these are shared resources — every backend module and every
// decorator reads/returns them. Renaming or repurposing one is a breaking
// change across the whole family; add new ones, don't mutate existing.

package cache

import "errors"

// Sentinel errors. Adapters MUST return these (wrapped with %w is fine) so
// callers can errors.Is against them regardless of backend.
var (
	// ErrNotFound is returned by Get/GetMulti/TTL when a key is absent or
	// expired. Get MUST return (nil, ErrNotFound) on a miss, never (nil, nil).
	ErrNotFound = errors.New("cache: not found")

	// ErrUnsupported is returned when an adapter does not implement an
	// optional operation (e.g. Iterate on a backend that cannot scan).
	ErrUnsupported = errors.New("cache: operation not supported by adapter")

	// ErrSerialization wraps codec encode/decode failures.
	ErrSerialization = errors.New("cache: serialization failed")

	// ErrTimeout is returned when an operation exceeds its context deadline
	// and the adapter chooses to surface it as a typed error.
	ErrTimeout = errors.New("cache: operation timed out")

	// ErrCircuitOpen is returned by resilience wrappers when the breaker is open.
	ErrCircuitOpen = errors.New("cache: circuit breaker open")

	// ErrTooLarge is returned when a value exceeds the adapter's max value size.
	ErrTooLarge = errors.New("cache: value too large")

	// ErrKeyTooLong is returned when a key exceeds the adapter's max key length.
	ErrKeyTooLong = errors.New("cache: key too long")

	// ErrClosed is returned by any operation invoked after Close.
	ErrClosed = errors.New("cache: closed")
)
