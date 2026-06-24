# AGENTS.md — codebase map for AI agents

Read this first. Orientation map for `ubgo/cache` so a fresh agent knows what every part does and where to change things, without reading every file.

## What this repo is

`ubgo/cache` is a **bytes-level `Cache` contract plus the ergonomics layered on top**: write code against one interface and swap an in-memory LRU for Redis, Postgres, or a tiered L1/L2 cache without touching call sites. It adds typed generics, a single-flight `Remember` (TTL, refresh-ahead, SWR, stale-if-error, negative caching, jitter), a portable distributed `Locker`, pluggable codecs (incl. AES-GCM), cross-process invalidation, resilience decorators, observability, and a shared conformance suite. The core has **zero third-party dependencies**; backends and extras are separate modules. See `README.md` and `doc.go`.

## Modules

| Path | Module | Role | Deps |
|---|---|---|---|
| `.` | `github.com/ubgo/cache` | The `Cache` interface + ergonomics (Remember, typed, codecs, lock, invalidation, resilience, obs, conformance). | stdlib only |
| `contrib/cache-otel`, `cache-prom` | each own module | OpenTelemetry / Prometheus observability adapters (implement the obs hooks). | otel / prometheus |
| `contrib/codec-msgpack`, `codec-zstd`, `codec-protobuf` | each own module | Extra codecs. | the codec lib |

**Backends are separate repos** (not in this one): `github.com/ubgo/cache-mem` (LRU / W-TinyLFU), `cache-redis`, `cache-pg`, `cache-tiered`. They each implement the `Cache` interface and are validated by this repo's `cachetest` conformance suite. Go 1.24.

## Core files — what each owns

| File | Responsibility |
|---|---|
| `cache.go` | The `Cache` interface (bytes in/out; `ErrNotFound` on miss) + `Item`, `Stats`, `IterateOpts`. |
| `remember.go` / `remember_multi.go` | `Remember[T]` / `RememberMulti[T]` — load-through with single-flight + the pattern options (refresh-ahead, SWR, stale-if-error, negative, jitter). |
| `singleflight.go` | The in-process call-collapsing used by Remember. |
| `typed.go` | `Typed[T]`, `GetT[T]`, `SetT[T]`, `Memoize` — the generics layer over the bytes interface. |
| `codec.go` | `Codec` interface + `JSONCodec`/`GobCodec`/`RawCodec`. |
| `encrypt.go` | `EncryptedCodec` (AES-GCM) for caching PII/secrets in shared stores. |
| `namespace.go` | `Namespaced` — key-prefix isolation so many services share one backend. |
| `lock.go` | `Locker` — a portable distributed lock built on `SetNX` (works on any adapter). |
| `invalidation.go` | Cross-process invalidation fan-out (drop stale L1 copies). |
| `resilience.go` / `bulkhead.go` | Circuit breaker, retry-with-backoff, bulkhead (concurrency limit) decorators. |
| `obs.go` / `stats.go` / `nsstats.go` | `Instrument`/`ObsHooks`, global + per-namespace stats. Events carry a key *hash*, never the raw key. |
| `hotkeys.go` | Hot-key detection (Space-Saving) — find the key that's most of your traffic. |
| `audit.go` | Tamper-aware audit log for mutations. |
| `warm.go` / `helpers.go` / `errors.go` / `doc.go` | Preload helpers, misc, sentinel errors, package overview. |

| Dir | Role |
|---|---|
| `cachetest/` | The exported **conformance suite** (`Run`) + a correct in-memory `Mock` + a Zipfian/hit-rate harness. Every adapter runs this. |
| `admin/` | HTTP inspection endpoint. |
| `docs/`, `scripts/` | Docs and tooling. |

## Conventions

- **Zero third-party deps in core.** Observability and extra codecs are `contrib/` modules; backends are separate repos.
- **Bytes in/out at the contract**; typing happens in the `typed.go` layer via a `Codec`.
- **Never log raw keys** (PII) — obs events carry a `KeyHash`.
- Every backend must pass `cachetest.Run` so behavior is identical across adapters.

## Where to look for X

- "Read-through with a loader" → `remember.go` (`Remember[T]`).
- "Add a backend" → a new `cache-<x>` repo implementing `cache.Cache`; validate with `cachetest.Run`.
- "Cache secrets safely" → `encrypt.go` (`EncryptedCodec`).
- "Per-tenant isolation" → `namespace.go`.
- "Metrics / tracing" → `obs.go` + `contrib/cache-prom` / `cache-otel`.
- "A distributed lock" → `lock.go` (`Locker`).
