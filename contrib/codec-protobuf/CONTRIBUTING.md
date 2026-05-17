# Contributing to codec-protobuf

`codec-protobuf` is an **independent Go module** at `contrib/codec-protobuf` inside the `ubgo/cache` repository. It is released together with the parent `ubgo/cache` module and tracks the `cache.Codec` / `cache.ErrSerialization` API of the version it ships with.

## Local development setup

It imports `github.com/ubgo/cache`. To build against an unreleased core, add a local `replace`:

```sh
go mod edit -replace github.com/ubgo/cache=../..
go mod tidy
```

(`../..` reaches the repo root where the core module lives.) Don't commit a machine-specific path unless the release process expects it.

## Build / test / lint gate

From inside `contrib/codec-protobuf`:

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

All four clean: zero `gofmt` diff, zero build errors, zero test failures (race on), zero lint findings.

## Doc-comment style

- Exported symbols (`Codec`, `Name`, `Encode`, `Decode`) get doc comments starting with the symbol name (revive enforces this).
- The package doc comment includes a runnable `cache.Remember` + `WithCodec` snippet and states the `proto.Message` requirement.
- Inline comments explain **why** — chiefly the type-assertion-before-marshal fail-fast and the `cache.ErrSerialization` wrapping.
- Doc PRs change comments only, never behavior.

## Notes

- The `proto.Message` type assertion MUST stay before marshal/unmarshal — it converts a programmer error into a recognizable `cache.ErrSerialization` instead of cache corruption / panic.
- Keep the `%w: ... : %v`/`%T` wrapping shape; callers branch on `errors.Is(err, cache.ErrSerialization)`.
- `Codec` is a stateless zero value; the `var _ cache.Codec = Codec{}` assertion must remain.
- Update `README.md` and `CHANGELOG.md` alongside any behavior change.
