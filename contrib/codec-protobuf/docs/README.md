# codec-protobuf — documentation

Per-feature cookbook for `github.com/ubgo/cache/contrib/codec-protobuf`, a Protocol Buffers `cache.Codec` for [`github.com/ubgo/cache`](https://github.com/ubgo/cache).

The package exports a single zero-field type, `Codec`, implementing the three `cache.Codec` methods. Values must be `proto.Message`; non-proto values fail with `cache.ErrSerialization` rather than corrupting the cache. Each export has a section in [`features.md`](./features.md).

## Index

| Symbol | Kind | What it is |
|---|---|---|
| [`Codec`](./features.md#codec) | type | A zero-field Protobuf codec implementing `cache.Codec`. |
| [`Codec.Name`](./features.md#codecname) | method | Returns the codec identifier `"protobuf"`. |
| [`Codec.Encode`](./features.md#codecencode) | method | Marshals a `proto.Message`; rejects non-proto with `cache.ErrSerialization`. |
| [`Codec.Decode`](./features.md#codecdecode) | method | Unmarshals into a `proto.Message` pointer; rejects non-proto with `cache.ErrSerialization`. |

## See also

- [`features.md`](./features.md) — full per-symbol cookbook, including the schema-evolution use case.
- Module [`README.md`](../README.md) — overview and rationale.
- Core [`cache`](https://github.com/ubgo/cache) docs for `cache.WithCodec` / `cache.Codec`.
