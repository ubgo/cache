# Contributing to codec-zstd

`codec-zstd` is an **independent Go module** at `contrib/codec-zstd` inside the `ubgo/cache` repository. It is released together with the parent `ubgo/cache` module and tracks the `cache.Codec` / `cache.ErrSerialization` API of the version it ships with.

## Local development setup

It imports `github.com/ubgo/cache`. To build against an unreleased core, add a local `replace`:

```sh
go mod edit -replace github.com/ubgo/cache=../..
go mod tidy
```

(`../..` reaches the repo root where the core module lives.) Don't commit a machine-specific path unless the release process expects it.

## Build / test / lint gate

From inside `contrib/codec-zstd`:

```sh
gofmt -w .
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

All four clean: zero `gofmt` diff, zero build errors, zero test failures (race on), zero lint findings.

## Doc-comment style

- Exported symbols (`Codec`, `Option`, `WithMinBytes`, `New`, `Name`, `Encode`, `Decode`) get doc comments starting with the symbol name (revive enforces this).
- The package doc comment includes a runnable `New` + `WithCodec` snippet and explains the threshold + 1-byte header.
- Inline comments explain **why** — the size threshold rationale, the 1-byte framing, and why the header makes threshold changes backward-safe.
- Doc PRs change comments only, never behavior.

## Notes

- The framing is a hard contract: `0x00` = raw inner bytes, `0x01` = zstd frame of inner bytes. Changing these values breaks every previously cached entry. Treat the header as a persisted format.
- `New` must keep tolerating a `nil` inner codec (falls back to `cache.DefaultCodec`).
- `EncodeAll(plain, []byte{hdrZstd})` deliberately uses the header byte as the destination buffer so the header prefixes the frame in one call — keep that.
- Errors on empty/unknown/corrupt input MUST stay wrapped with `cache.ErrSerialization`.
- Update `README.md` and `CHANGELOG.md` alongside any behavior change.
