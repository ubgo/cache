# ubgo/cache — Feature Cookbook

Every exported feature of `github.com/ubgo/cache` (and its `cachetest` / `admin`
sub-packages), documented one-by-one: what it is, real-world use cases, and a
runnable Go snippet.

## Start here

1. Pick a backend module (`cache-mem`, `cache-redis`, `cache-pg`, `cache-tiered`, …). It implements [`cache.Cache`](./core-operations.md).
2. For raw bytes, use the [core operations](./core-operations.md) (`Get`/`Set`/`Del`/…).
3. For typed load-through with stampede protection, use [`Remember`](./remember.md) or a [`Typed[T]`](./generics.md) view.
4. Compose cross-cutting behavior by wrapping the cache: [namespacing](./namespacing.md), [observability](./observability.md), [resilience](./resilience.md).
5. Unit-test consumers with [`cachetest.Mock`](./testing.md); adapter authors run [`cachetest.Run`](./testing.md).

All snippets use `mem "github.com/ubgo/cache-mem"` as a stand-in backend; swap
in any adapter — behavior is identical (enforced by the conformance suite).

## Pages

| Page | Covers |
|---|---|
| [core-operations.md](./core-operations.md) | `Cache` interface (all 19 methods), `Item`, `IterateOpts`, `Iterator`, sentinel errors, `Stats`, `EvictionCause`, `HitRatio` |
| [remember.md](./remember.md) | `Remember`, `LoadFn`, all `RememberOpt`s, `GetT`/`SetT`, the envelope, `RememberMulti`/`MultiLoadFn` |
| [generics.md](./generics.md) | `Typed[T]`, `NewTyped`, its methods, `Raw` |
| [codecs.md](./codecs.md) | `Codec`, `JSONCodec`, `GobCodec`, `RawCodec`, `DefaultCodec`, `EncryptedCodec`, `KeyProvider`, `StaticKey` |
| [namespacing.md](./namespacing.md) | `Namespaced` + scoped `Flush` |
| [locker.md](./locker.md) | `Locker`, `NewLock`, `Acquire`/`Refresh`/`Release`, `ErrLockNotAcquired` |
| [invalidation.md](./invalidation.md) | `Invalidation`, `NewInProcInvalidation`, `InvalidateAll` |
| [observability.md](./observability.md) | `Instrument`, `ObsHooks`, `OpEvent`, `KeyHash`, `HotKeyTracker`, `NamespaceStats`, `NamespaceFn`, `DefaultNamespaceFn` |
| [resilience.md](./resilience.md) | `NewCircuitBreaker`, `NewRetry`, `NewBulkhead`, `NewAuditLog`, `AuditEvent`, `AuditFunc`, all options, stacking order |
| [helpers.md](./helpers.md) | `Memoize`, `QueryKey`, `Once`, `ErrInFlight`, `Warm`, `WarmResult` |
| [admin.md](./admin.md) | `admin.Mount`, `admin.Handler`, `admin.Options`, routes, auth gating |
| [testing.md](./testing.md) | `cachetest.Run`, `Bench`, `Factory`, `Mock`, `Zipfian`/`NewZipfian`, `ScanGen`, `HitRate`, `Round` |

## Capability matrix

| I want to… | Use | Page |
|---|---|---|
| Store/read raw bytes | `Set` / `Get` | [core](./core-operations.md) |
| Cache a typed value with load-through | `Remember[T]` | [remember](./remember.md) |
| Keep hot keys from ever expiring | `WithRefreshAhead` | [remember](./remember.md) |
| Stay up when the DB is down | `WithStaleIfError` | [remember](./remember.md) |
| Stop re-querying missing rows | `WithNegativeTTL` | [remember](./remember.md) |
| Avoid a thundering-herd expiry | `WithJitter` | [remember](./remember.md) |
| Batch-load N keys in 2 round-trips | `RememberMulti[T]` | [remember](./remember.md) |
| A type-safe cache handle | `Typed[T]` | [generics](./generics.md) |
| Encrypt PII at rest in a shared store | `EncryptedCodec` | [codecs](./codecs.md) |
| Share one backend across services | `Namespaced` | [namespacing](./namespacing.md) |
| A cross-pod mutex | `NewLock` | [locker](./locker.md) |
| Drop L1 copies when another node writes | `Invalidation` | [invalidation](./invalidation.md) |
| Metrics / tracing without a dependency | `Instrument` + `ObsHooks` | [observability](./observability.md) |
| Per-feature hit-rate | `NamespaceStats` | [observability](./observability.md) |
| Find the key that is 60% of traffic | `HotKeyTracker` | [observability](./observability.md) |
| Fail fast when the backend is sick | `NewCircuitBreaker` | [resilience](./resilience.md) |
| Retry transient backend errors | `NewRetry` | [resilience](./resilience.md) |
| Bound in-flight load on the backend | `NewBulkhead` | [resilience](./resilience.md) |
| A compliance trail of writes | `NewAuditLog` | [resilience](./resilience.md) |
| Memoize a function | `Memoize` | [helpers](./helpers.md) |
| Idempotent webhooks/payments | `Once` | [helpers](./helpers.md) |
| Preload a cold cache at startup | `Warm` | [helpers](./helpers.md) |
| A collision-free cache key | `QueryKey` | [helpers](./helpers.md) |
| Inspect cache in production over HTTP | `admin.Mount` | [admin](./admin.md) |
| Unit-test code that uses a cache | `cachetest.Mock` | [testing](./testing.md) |
| Verify a new adapter is correct | `cachetest.Run` | [testing](./testing.md) |
