# Contributing to cache-otel

`cache-otel` is an **independent Go module** at `contrib/cache-otel` inside the `ubgo/cache` repository. It is released together with the parent `ubgo/cache` module and tracks the `cache.ObsHooks` API of the version it ships with.

## Local development setup

It imports `github.com/ubgo/cache`. To build against an unreleased core, add a local `replace`:

```sh
go mod edit -replace github.com/ubgo/cache=../..
go mod tidy
```

(`../..` reaches the repo root where the core module lives.) Don't commit a machine-specific path unless the release process expects it.

## Build / test / lint gate

From inside `contrib/cache-otel`:

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

All four clean: zero `gofmt` diff, zero build errors, zero test failures (race on), zero lint findings.

Tests use a `sdkmetric.ManualReader` so metrics can be collected and asserted synchronously without an exporter — no OTLP collector required.

## Doc-comment style

- Exported symbols (`New`) get a doc comment starting with the symbol name (revive enforces this).
- The package doc comment lists the instruments and a runnable wiring snippet.
- Inline comments explain **why** — the result-classification order, and the per-call attribute-slice copies (avoiding aliasing of the shared base slice).
- Doc PRs change comments only, never behavior.

## Notes

- Keep the result taxonomy `ok` / `miss` / `error`.
- `cache.op.duration` is seconds (`WithUnit("s")`, `Duration.Seconds()`); keep them consistent.
- Instrument-creation errors are returned, never panicked.
- Update `README.md` and `CHANGELOG.md` alongside any behavior change.
