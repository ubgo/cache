# Contributing to codec-msgpack

`codec-msgpack` is an **independent Go module** at `contrib/codec-msgpack` inside the `ubgo/cache` repository. It is released together with the parent `ubgo/cache` module and tracks the `cache.Codec` / `cache.ErrSerialization` API of the version it ships with.

## Local development setup

It imports `github.com/ubgo/cache`. To build against an unreleased core, add a local `replace`:

```sh
go mod edit -replace github.com/ubgo/cache=../..
go mod tidy
```

(`../..` reaches the repo root where the core module lives.) Don't commit a machine-specific path unless the release process expects it.

## Build / test / lint gate

From inside `contrib/codec-msgpack`:

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

All four clean: zero `gofmt` diff, zero build errors, zero test failures (race on), zero lint findings.

## Doc-comment style

- Exported symbols (`Codec`, `Name`, `Encode`, `Decode`) get doc comments starting with the symbol name (revive enforces this).
- The package doc comment includes a runnable `cache.Remember` + `WithCodec` snippet.
- Inline comments explain **why** — chiefly why every error is wrapped with `cache.ErrSerialization` (so callers can `errors.Is`).
- Doc PRs change comments only, never behavior.

## Notes

- Every encode/decode failure MUST stay wrapped with `cache.ErrSerialization` via `%w`. Callers branch on `errors.Is`.
- `Codec` is a stateless zero value — keep it allocation-free and goroutine-safe.
- The `var _ cache.Codec = Codec{}` assertion must remain (compile-time interface check).
- Update `README.md` and `CHANGELOG.md` alongside any behavior change.
