# Changelog

All notable changes to `github.com/ubgo/cache` are documented here. Format
follows Keep a Changelog; the project follows SemVer (pre-GA in `v0.x`, where
minor bumps may break — see PLAN §13).

## [Unreleased]

### Added

- Core `Cache` interface (bytes-level): Get/GetMulti/Has/TTL, Set/SetMulti/
  SetNX/Expire/Touch, Incr/Decr, Del/DeleteByPrefix/Flush, Iterate, Ping,
  Close, Stats.
- Sentinel errors (`ErrNotFound`, `ErrUnsupported`, `ErrSerialization`, …).
- Codecs: JSON (default), Gob, Raw passthrough.
- `Namespaced` view with prefix-scoped `Flush`.
- Single-flight `Remember[T]` with `WithRefreshAhead`, `WithStaleWhileRevalidate`,
  `WithStaleIfError`, `WithNegativeTTL`, `WithJitter`, `WithCodec`.
- `GetT` / `SetT` / `Typed[T]` generics layer.
- `Locker` (`NewLock`) — portable distributed lock on `SetNX`, token-checked
  release, owner-checked refresh.
- Zero-dependency observability: `Instrument` + `ObsHooks` + privacy-safe
  `KeyHash`.
- `cachetest.Run` conformance suite, `cachetest.Bench`, and `cachetest.Mock`
  (correct reference impl with failure injection).
- `cachetest` hit-rate harness: `NewZipfian`, `ScanGen`, `HitRate` for
  comparable eviction-policy quality benchmarks across adapters.
- `cache/admin`: dependency-free HTTP surface (stats, single-key inspect,
  auth-gated evict).
- `contrib/cache-prom`: Prometheus exporter (separate module).
- `contrib/cache-otel`: OpenTelemetry metrics exporter (separate module).
- `EncryptedCodec`: AES-GCM authenticated encryption wrapper over any codec
  (stdlib only; tamper + wrong-key detection).
- `contrib/codec-msgpack`: MessagePack codec (separate module).
- `contrib/codec-zstd`: size-thresholded zstd compression wrapper (separate
  module).
- `Invalidation` interface + `NewInProcInvalidation` in-process bus +
  `InvalidateAll` sentinel for cross-process L1 invalidation.
- Helpers: `Memoize[K,V]` (cache-backed memoation), `QueryKey`
  (collision-resistant key builder), `Once`/`ErrInFlight` (idempotency-key
  guard for dedup of side-effecting ops).
- `RememberMulti[T]` / `MultiLoadFn`: batch load-through that collapses the
  per-key N+1 into one GetMulti + one loader call + one SetMulti.
- `HotKeyTracker`: Space-Saving decorator surfacing approximate top-N hottest
  read keys with bounded memory.
- `NamespaceStats`: per-namespace hit/miss/set/delete breakdown decorator
  (`DefaultNamespaceFn` = prefix-before-colon; pluggable).
- Resilience decorators: `NewCircuitBreaker` (threshold/cooldown/half-open)
  and `NewRetry` (attempts + exponential backoff). ErrNotFound/ErrUnsupported
  are not treated as failures; context cancellation aborts retries.
- `NewAuditLog`: emits an `AuditEvent` (op/keys/err/time) for every
  state-changing operation; reads are not audited.
- `NewBulkhead`: semaphore-bounded max in-flight ops with context-aware
  acquire (saturation surfaces as ctx error, not goroutine growth).
- `Warm[T]`: bounded-concurrency startup preload with per-key error
  reporting and context cancellation.

[Unreleased]: https://github.com/ubgo/cache/commits/main
