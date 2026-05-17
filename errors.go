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
