# codec-zstd — feature cookbook

Every exported identifier in `github.com/ubgo/cache/contrib/codec-zstd`, with concrete use cases and runnable snippets.

Import paths used throughout:

```go
import (
    "github.com/ubgo/cache"
    codeczstd "github.com/ubgo/cache/contrib/codec-zstd"
)
```

---

### New

```go
func New(inner cache.Codec, opts ...Option) *Codec
```

Wraps `inner` with size-thresholded zstd compression and returns a `*Codec`. If `inner` is `nil`, `cache.DefaultCodec` (JSON) is used. The default threshold is **16 KiB** (`1 << 14`). The zstd encoder/decoder are created once and reused (one-shot `EncodeAll`/`DecodeAll` mode, which is concurrency-safe).

**Use cases:**

- Cut Redis memory and network for large cached values (rendered HTML, big JSON blobs) while keeping your existing serialization.
- Layer compression onto msgpack/protobuf without changing call sites — wrap and pass via `cache.WithCodec`.
- Default sensibly: pass `nil` inner to compress JSON-encoded values out of the box.

```go
// Compress JSON-encoded values over the default 16 KiB threshold.
codec := codeczstd.New(cache.JSONCodec{})

ctx := context.Background()
v, err := cache.Remember(ctx, c, "page:home", time.Minute,
	func(ctx context.Context) (string, error) { return renderHomePage(), nil },
	cache.WithCodec(codec),
)
_ = v
_ = err
```

```go
// nil inner => cache.DefaultCodec (JSON).
codec := codeczstd.New(nil)
fmt.Println(codec.Name()) // zstd+json
```

---

### Option

```go
type Option func(*Codec)
```

Functional-option type consumed by `New`. The only built-in option is `WithMinBytes`; the type is exported so callers can hold/pass options as values or build their own slices.

**Use cases:**

- Build a reusable `[]codeczstd.Option` config and apply it to several codecs.
- Pass options conditionally (e.g. a smaller threshold in a memory-constrained environment).

```go
opts := []codeczstd.Option{codeczstd.WithMinBytes(4 << 10)}
codec := codeczstd.New(cache.JSONCodec{}, opts...)
_ = codec
```

---

### WithMinBytes

```go
func WithMinBytes(n int) Option
```

Sets the threshold, in **inner-encoded bytes**, above which zstd is applied. Values whose encoded length is **at or below** `n` are stored uncompressed. Default 16 KiB.

**Use cases:**

- Lower the threshold (e.g. 4 KiB) when values are moderately sized but numerous and memory-bound.
- Raise the threshold when CPU is the bottleneck and only very large values are worth compressing.
- Safe to change **after** data is written — see Decode below; old entries still decode via the path they were written with.

```go
codec := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(1<<12)) // 4 KiB
_ = codec
```

#### Threshold rationale

The threshold is on the **inner-encoded** length, not the original value size. Below ~16 KiB, the zstd frame header + dictionary overhead plus the CPU cost of (de)compression typically outweigh any size saving — a small payload can even get *larger*. Storing such values raw (with a 1-byte header) is strictly cheaper. The default 16 KiB is a conservative general-purpose break-even point; tune per workload with measurements, not guesses.

---

### Codec

```go
type Codec struct{ /* unexported fields */ }
```

The wrapping codec returned by `New`. Implements `cache.Codec`. Holds the inner codec, the threshold, and the reused zstd encoder/decoder. Use the `*Codec` returned by `New`; do not construct a zero value directly (its encoder/decoder would be nil).

**Use cases:**

- Pass to `cache.WithCodec` anywhere a codec is accepted.
- Compose: the inner codec can itself be msgpack/protobuf.

```go
codec := codeczstd.New(cache.JSONCodec{})
c := cache.NewTyped[Page](backend, cache.WithCodec(codec))
_ = c
```

---

### Codec.Name

```go
func (c *Codec) Name() string // "zstd+" + inner.Name()
```

Returns `"zstd+"` concatenated with the inner codec's name, e.g. `"zstd+json"`, `"zstd+msgpack"`, `"zstd+protobuf"`.

**Use cases:**

- Self-documenting log/metric label showing both compression and serialization.
- Assert the full codec stack is wired as expected before serving traffic.

```go
fmt.Println(codeczstd.New(cache.JSONCodec{}).Name()) // zstd+json
```

---

### Codec.Encode

```go
func (c *Codec) Encode(v any) ([]byte, error)
```

Serializes `v` via the inner codec, then: if `len(plain) <= min`, returns `0x00 || plain` (raw); otherwise returns `0x01 || zstd(plain)`. Inner-codec errors already carry `cache.ErrSerialization` and pass through unchanged (no double-wrapping).

**Use cases:**

- Transparent compression on writes — call sites are unchanged.
- Inner-error transparency: a non-encodable value still surfaces as `cache.ErrSerialization` from the inner codec.

```go
codec := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(8))

small, _ := codec.Encode("hi")              // <= 8 bytes encoded -> 0x00 || json
fmt.Printf("%#x\n", small[0])               // 0x0

big, _ := codec.Encode(strings.Repeat("A", 1000))
fmt.Printf("%#x\n", big[0])                 // 0x1 (zstd frame follows)
```

#### Framing header (0x00 / 0x01)

The first byte of every stored value is a **persisted on-disk format**:

| Header | Meaning |
|---|---|
| `0x00` (`hdrRaw`) | remaining bytes are raw inner-codec output |
| `0x01` (`hdrZstd`) | remaining bytes are a zstd frame of inner-codec output |

These constants are **frozen**: changing them would make every previously cached entry undecodable. `EncodeAll` appends the zstd frame onto the supplied buffer, so passing `{0x01}` as the destination yields `0x01 || frame` in a single call.

---

### Codec.Decode

```go
func (c *Codec) Decode(data []byte, v any) error
```

Reverses `Encode`. Empty input → `cache.ErrSerialization` ("empty zstd payload"). Otherwise dispatches **purely on the stored header byte**, never on `c.min`:

- `0x00` → inner-decode the body directly.
- `0x01` → zstd-decompress the body, then inner-decode.
- anything else → `cache.ErrSerialization` ("unknown zstd header 0x..").

**Use cases:**

- Round-trip values through `cache.Remember` / `cache.GetT` with compression invisible to callers.
- Change `WithMinBytes` after data is already written: header-only dispatch means old entries decode via whatever path they were written with — no flush needed.
- Treat truncated/foreign/corrupt entries as a typed `cache.ErrSerialization` so callers reload instead of crashing.

```go
codec := codeczstd.New(cache.JSONCodec{})

var page Page
if err := codec.Decode(stored, &page); err != nil {
	if errors.Is(err, cache.ErrSerialization) {
		// empty / unknown header / zstd corruption / inner decode failure
	}
}
```

---

### Compose-with-encrypt ordering

`codec-zstd` wraps an inner codec; encryption is also typically a codec wrapper. **Order matters — always compress *before* you encrypt:**

```
value ──► inner (json/msgpack/proto) ──► zstd ──► encrypt ──► store
```

```go
// Conceptual stacking: zstd wraps the serializer, encryption wraps zstd.
serial := cache.JSONCodec{}
compressed := codeczstd.New(serial)
secure := cache.NewEncryptedCodec(compressed, key) // hypothetical encrypt wrapper
c := cache.NewTyped[Page](backend, cache.WithCodec(secure))
_ = c
```

Rationale: ciphertext is high-entropy and effectively incompressible, so compressing *after* encrypting saves nothing. Compressing *before* encrypting shrinks the plaintext first, then encrypts the smaller result — you get both the size win and confidentiality. (Be aware of the general compression-before-encryption side-channel class, e.g. CRIME/BREACH, when the same secret protects attacker-influenced and secret data in one entry; for opaque cache blobs this is normally not a concern, but document it where it could be.)
