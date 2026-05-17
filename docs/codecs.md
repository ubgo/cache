# Codecs — serialization

A `Codec` turns typed values into the bytes the cache stores. It is used by
every typed entry point (`Remember`, `GetT`/`SetT`, `Typed[T]`,
`RememberMulti`, `Memoize`, `Once`, `Warm`) and is selected with
[`WithCodec`](./remember.md).

```go
import (
	"github.com/ubgo/cache"
	mem "github.com/ubgo/cache-mem"
)
```

---

### `Codec` interface

```go
type Codec interface {
	Encode(v any) ([]byte, error)
	Decode(data []byte, v any) error
	Name() string
}
```

Use cases: bring your own serializer (msgpack, protobuf, zstd-wrapped JSON —
several ship in `contrib/`).

```go
type MyCodec struct{}
func (MyCodec) Name() string                 { return "mine" }
func (MyCodec) Encode(v any) ([]byte, error)  { /* ... */ return nil, nil }
func (MyCodec) Decode(b []byte, v any) error  { /* ... */ return nil }

var _ cache.Codec = MyCodec{}
```

### `JSONCodec`

The default codec: portable and debuggable. `Name()` is `"json"`. Encode
failures wrap `ErrSerialization`.

Use cases: cross-language stores; values you want to eyeball in `redis-cli`.

```go
cache.Remember(ctx, c, "k", 0, load, cache.WithCodec(cache.JSONCodec{}))
```

### `GobCodec`

Faster for Go-only struct round-trips. `Name()` is `"gob"`. Types with
unexported fields must `gob.Register` as usual.

Use cases: hot path, Go-only services, structs with many fields where JSON
overhead matters.

```go
cache.SetT(ctx, c, "k", myStruct, time.Minute, cache.WithCodec(cache.GobCodec{}))
```

### `RawCodec`

Passthrough for already-serialized payloads. Only accepts
`[]byte`/`*[]byte`/`string`/`*string`; anything else is an `ErrSerialization`
so misuse fails loudly instead of silently corrupting data. `Name()` is
`"raw"`.

Use cases: you already have protobuf/marshalled bytes; caching a rendered HTML
string; avoiding double-encoding.

```go
html := "<nav>…</nav>"
_ = cache.SetT(ctx, c, "frag:nav", html, time.Minute, cache.WithCodec(cache.RawCodec{}))
got, _ := cache.GetT[string](ctx, c, "frag:nav", cache.WithCodec(cache.RawCodec{}))
_ = got
```

### `DefaultCodec`

Package variable used by the generics layer when no codec is configured:
`var DefaultCodec Codec = JSONCodec{}`.

Use cases: read what the default is; (advanced) set a process-wide default
before any caching starts.

```go
_ = cache.DefaultCodec.Name() // "json"
```

---

## Encryption at rest

### `KeyProvider`

`type KeyProvider func() ([]byte, error)` — supplies the 16/24/32-byte AES key.
Returning a fresh value each call enables rotation; decryption always uses the
current key (rotate by re-warming the cache, not in place).

### `StaticKey(key []byte) KeyProvider`

A `KeyProvider` for a fixed key.

```go
kp := cache.StaticKey([]byte("0123456789abcdef")) // 16 bytes = AES-128
```

### `EncryptedCodec`

Wraps another `Codec` with authenticated encryption (AES-GCM). Cache
PII/secrets in a shared or managed backend so a raw dump of the store is not a
data breach.

- Wire format: `[12-byte random nonce][GCM ciphertext+tag]`. A fresh nonce per `Encode`.
- Tampered ciphertext or wrong key → GCM authentication fails → surfaces as `ErrSerialization` on `Decode`.
- `Name()` is `"aesgcm+" + inner.Name()`. A nil `Inner` falls back to `DefaultCodec`.
- Rotation: swap the key in your `KeyProvider`; entries written with the old key can no longer be decrypted (treat as a miss / re-warm).

Use cases: cache user PII in shared Redis; cache decrypted secrets/tokens
briefly without exposing them in a memory dump or backup.

```go
codec := cache.EncryptedCodec{
	Inner: cache.JSONCodec{},
	Key:   cache.StaticKey(key32),
}

_ = cache.SetT(ctx, c, "pii:user:42", profile, time.Minute,
	cache.WithCodec(codec))

p, err := cache.GetT[Profile](ctx, c, "pii:user:42", cache.WithCodec(codec))
if errors.Is(err, cache.ErrSerialization) {
	// tampered, or written under a rotated key — treat as a miss
}
_ = p
```
