# Contributing to cache-prom

`cache-prom` is an **independent Go module** living at `contrib/cache-prom` inside the `ubgo/cache` repository. It is released together with the parent `ubgo/cache` module and tracks the `cache.ObsHooks` API of the version it ships with.

## Local development setup

The module imports `github.com/ubgo/cache`. When working against an unreleased core, add a local `replace` so you build against the sibling working tree:

```sh
go mod edit -replace github.com/ubgo/cache=../..
go mod tidy
```

(`../..` because `contrib/cache-prom` is two directories below the repo root where the core module lives.) Remove or keep the `replace` according to the release process — do not commit a machine-specific path.

## Build / test / lint gate

Every change must pass, from inside `contrib/cache-prom`:

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

All four clean: zero `gofmt` diff, zero build errors, zero test failures (race on), zero lint findings.

## Doc-comment style

- Exported symbols (`New`) get a doc comment starting with the symbol name (revive enforces this).
- The package doc comment lists the registered metrics and a runnable wiring snippet.
- Inline comments explain **why** — especially the result-classification branch (why `ErrNotFound` is a `miss`, not an `error`).
- Do not change behavior in a doc PR; comments only.

## Notes

- Keep the result taxonomy stable: `ok` / `miss` / `error`. Dashboards and alerts depend on it.
- `adapter` / `namespace` must remain constant labels so multiple caches share a registry.
- Registration failures are returned, never panicked — preserve that.
- Update `README.md` and `CHANGELOG.md` alongside any behavior change.
