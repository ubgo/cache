# Contributing to ubgo/cache

Thanks for contributing. This module is the core contract for the entire `ubgo/cache-*` ecosystem, so changes here ripple into every adapter. Please read the contract section before touching the `Cache` interface.

## Build, test, lint

The core module has **zero third-party dependencies**. The full local gate, run from the module root:

```sh
gofmt -w .                       # format
go build ./...                   # must compile
go test -race -count=1 ./...     # race detector on
golangci-lint run ./...          # must report 0 issues
```

Or via [Task](https://taskfile.dev/):

```sh
task fmt          # gofmt -w .
task test:race    # go test -race -count=2 ./...
task lint         # golangci-lint run ./...
task check        # fmt:check + vet + race tests (the pre-PR gate)
task bench        # conformance benchmarks against the mock
```

CI runs the same commands. A PR must be `gofmt`-clean, build, pass `go test -race ./...`, and produce **0** `golangci-lint` issues (`errcheck`, `govet`, `staticcheck`, `revive`, `gocritic`, `misspell`, `unused`, `ineffassign`, `unconvert`). The only configured exclusion is the unused `ctx` parameter, which interface compliance forces adapters to keep.

## The conformance-suite contract

`cachetest.Run` **is** the `Cache` contract. Any change to interface semantics must be reflected as a subtest there, and every adapter must keep passing it. A new adapter is "correct" iff:

```go
func TestConformance(t *testing.T) {
    cachetest.Run(t, func(t *testing.T) cache.Cache { return mybackend.New() })
}
```

passes. Non-negotiable invariants the suite enforces:

- `Get` returns `(nil, ErrNotFound)` on miss/expiry — never `(nil, nil)`.
- `ttl <= 0` means no expiry.
- `SetNX` returns `true` only when it created the key.
- `Incr`/`Decr` are atomic; a missing key starts at `0`.
- `Close` is idempotent; ops after `Close` return `ErrClosed`.
- Optional ops an adapter can't serve return `ErrUnsupported` (and `Iterate` returns an iterator whose first `Next()` is false with `Err() == ErrUnsupported`).

If you change a guarantee, you change `cachetest` first, then every adapter — in lockstep. The reference implementation is `cachetest.Mock`; it must always pass its own suite.

## Local multi-module dev setup

Adapters and `contrib/*` modules import this module via a local `replace` directive so you can develop the contract and a consumer together:

```
// in cache-redis/go.mod or contrib/cache-prom/go.mod
replace github.com/ubgo/cache => ../../
```

When you change the core interface, run the gate **here**, then run the gate in each affected sibling/contrib module against your local checkout (the `replace` makes them pick it up). Do not commit a core change that breaks a sibling's conformance run. Never edit `go.mod` to remove or rewrite a `replace` directive as part of an unrelated change.

`contrib/*` modules have their own `go.mod` and may pull third-party deps (Prometheus, OpenTelemetry, zstd, ...). That is the whole point of keeping them separate: the core stays dependency-free. Do **not** add a third-party import to the core module — if you need one, it belongs in a new `contrib/` module.

## Code style

- **Doc comments on every exported identifier** (`revive` enforces this). Start the comment with the identifier name (`// Remember is ...`).
- Comments explain **why** and the **invariant/edge case**, not what the code obviously does. Document multi-shape fields and non-obvious framework rules at the declaration, not in a sidecar doc.
- No manual line-wrapping inside doc-comment prose paragraphs beyond what gofmt does; keep example blocks runnable.
- `gofmt` is authoritative; do not hand-format.
- Keep the hot path allocation-light; benchmark with `task bench` if you touch `Remember`, the codec path, or single-flight.
- Decorators must implement the full `Cache` interface and add a `var _ Cache = (*T)(nil)` assertion.

## Commit & PR conventions

- One logical change per commit; present-tense, imperative subject (`add stale-if-error to Remember`, `fix Warm cancellation race`).
- PR description states: what changed, why, and whether the `Cache` contract or `cachetest` changed (call this out explicitly — reviewers gate on it).
- Include/adjust tests in the same PR. Contract changes require a `cachetest` subtest.
- The PR must be green on the full gate (`task check` + `golangci-lint`) before review.
- Do not bundle unrelated refactors with a contract change.

## Reporting issues

Include the adapter + version, a minimal reproducer (the `cachetest.Mock` is ideal for isolating core bugs), and whether `cachetest.Run` still passes for the affected backend.
