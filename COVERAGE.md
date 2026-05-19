# Test Coverage Report — the ubgo/cache family

Generated from `go test -covermode=atomic ./...` per module (Go 1.24).
Regenerate any module's number with `task cover` in that module's directory.
CI enforces a per-repo coverage floor (see each repo's `test.yml`), so these
numbers cannot silently regress.

## Family summary

| Module | Coverage | CI floor |
|---|---:|---:|
| `github.com/ubgo/cache` (module total) | **96.1%** | 93% |
| `github.com/ubgo/cache-mem` | **95.2%** | 92% |
| `github.com/ubgo/cache-redis` | **96.1%** | 93% |
| `github.com/ubgo/cache-pg` | **98.0%** | 95% |
| `github.com/ubgo/cache-memcached` | **100.0%** | 97% |
| `github.com/ubgo/cache-cluster` | **98.8%** | 95% |
| `github.com/ubgo/cache-tiered` | **100.0%** | 97% |
| `github.com/ubgo/cache-cli` | **98.8%** | 95% |
| `contrib/cache-prom` | **92.9%** | 89% |
| `contrib/cache-otel` | **94.1%** | 91% |
| `contrib/codec-msgpack` | **100.0%** | 97% |
| `contrib/codec-zstd` | **100.0%** | 97% |
| `contrib/codec-protobuf` | **92.9%** | 89% |

**Family average ≈ 97%** (unweighted mean of module totals). Every module is
also `go test -race` clean and `golangci-lint`-clean.

## Core module (`github.com/ubgo/cache`) per package

| Package | Coverage |
|---|---:|
| `cache` (interface, Remember, codecs, decorators, helpers, Locker, Invalidation) | **99.3%** |
| `cache/admin` (HTTP inspection surface) | **100.0%** |
| `cache/cachetest` (conformance suite + Mock + Zipfian/HitRate harness) | **89.4%** |

`cachetest` is lower by design: it is the *test harness itself* — `Bench`,
`HitRate`, `ScanGen`, `Zipfian` and the `Run`/`Mock` `ErrUnsupported`/`Skip`
branches are exercised by real adapters in sibling modules, not by the core
module's own `go test`.

## Justified-uncovered remainder

The residual few percent in each module is **reachable only via real OS/IO
faults or states the Go runtime contract makes impossible**, with no existing
injection seam. We deliberately did not add production seams purely to chase
100%. Representative items:

- `bulkhead.go` `acquire` `ErrTimeout` — needs `ctx.Done()` fired while
  `ctx.Err()==nil`, which `context.Context` forbids.
- `encrypt.go` `Encode` — `io.ReadFull(rand.Reader, …)` error: real
  `crypto/rand` OS fault only.
- `cache-mem` `aof.go` — `os.OpenFile`/`Sync`/`Rename` mid-write failures and
  gob-encode errors on a fixed struct; OS-IO-fault-only or unreachable with a
  single captured clock.
- `cache-mem` `policy.go` — W-TinyLFU branches unreachable through the public
  API (e.g. a just-pushed key can never be the window LRU tail when
  `winCap≥1`).
- `cache-pg` `pgcache.go` — `tx.Commit()` failure after a successful
  DELETE+INSERT; not deterministically forceable on SQLite without a seam.
- `cache-redis` `invalidation.go` — pub/sub channel-closed branch (go-redis
  only closes it via the deferred `ps.Close()` inside `Subscribe`).
- `cache-cluster` `cluster.go` — the documented in-flight re-check race window
  and `http.NewRequestWithContext` error (method/URL always valid).
- `cache-cli` `main()` — the `os.Exit(run(...))` shell (run() is 100%).
- `contrib/cache-prom`,`cache-otel` — the `op=="get" && !Hit` nil-error
  branch, unreachable via `cache.Instrument` (it sets `hit = err==nil`).
- `contrib/codec-protobuf` `Encode` — `proto.Marshal` failure on a
  well-formed asserted `proto.Message`.

No production bug was found while reaching these numbers; all uncovered code
is error-handling for conditions the test environment cannot deterministically
produce.

## Reproduce

```sh
# any module
task cover
# or
go test -covermode=atomic -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -1
# HTML drill-down
go tool cover -html=cover.out
```
