# codec-msgpack — documentation

Per-feature cookbook for `github.com/ubgo/cache/contrib/codec-msgpack`, a MessagePack `cache.Codec` for [`github.com/ubgo/cache`](https://github.com/ubgo/cache).

The package exports a single zero-field type, `Codec`, with the three methods of the `cache.Codec` interface. Each export has a section in [`features.md`](./features.md) with concrete use cases and a runnable snippet.

## Index

| Symbol | Kind | What it is |
|---|---|---|
| [`Codec`](./features.md#codec) | type | A zero-field MessagePack codec implementing `cache.Codec`. |
| [`Codec.Name`](./features.md#codecname) | method | Returns the codec identifier `"msgpack"`. |
| [`Codec.Encode`](./features.md#codecencode) | method | Marshals a value to MessagePack; wraps failures as `cache.ErrSerialization`. |
| [`Codec.Decode`](./features.md#codecdecode) | method | Unmarshals MessagePack into a pointer; wraps failures as `cache.ErrSerialization`. |

## See also

- [`features.md`](./features.md) — full per-symbol cookbook, including JSON/Gob tradeoffs.
- Module [`README.md`](../README.md) — overview and rationale.
- Core [`cache`](https://github.com/ubgo/cache) docs for `cache.WithCodec` / `cache.Codec`.
