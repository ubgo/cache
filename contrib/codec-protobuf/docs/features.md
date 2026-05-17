# codec-protobuf — feature cookbook

Every exported identifier in `github.com/ubgo/cache/contrib/codec-protobuf`, with concrete use cases and runnable snippets.

Import paths used throughout:

```go
import (
    "github.com/ubgo/cache"
    codecprotobuf "github.com/ubgo/cache/contrib/codec-protobuf"
    "google.golang.org/protobuf/proto"
)
```

In the snippets, `pb.User` is a generated `proto.Message` type from your own `.proto` schema.

---

### Codec

```go
type Codec struct{}
```

A zero-field value type implementing `cache.Codec` using the Protobuf wire format. Construct with the literal `codecprotobuf.Codec{}` — no constructor, no configuration, safe to share and use concurrently. Only `proto.Message` values are valid; anything else is rejected up front.

**Use cases:**

- Cache values that are already generated Protobuf types shared with other services.
- Schema-evolvable cache entries: add/deprecate fields without breaking previously cached bytes.
- Compact, fast binary serialization with strong cross-service interop.

```go
backend := cachetest.NewMock()
c := cache.NewTyped[*pb.User](backend, cache.WithCodec(codecprotobuf.Codec{}))
_ = c
```

---

### Codec.Name

```go
func (Codec) Name() string // returns "protobuf"
```

Returns the stable codec identifier `"protobuf"`.

**Use cases:**

- Log/metric label for the active codec.
- Pre-flight assertion that the protobuf codec is wired.
- Composes into a wrapper codec's name (e.g. `codec-zstd` → `"zstd+protobuf"`).

```go
fmt.Println(codecprotobuf.Codec{}.Name()) // protobuf
```

---

### Codec.Encode

```go
func (Codec) Encode(v any) ([]byte, error)
```

Marshals a `proto.Message`. The `cache.Codec` interface accepts `any`, so the proto requirement is enforced at runtime: the type is asserted **first** and a non-proto value fails loudly with the offending `%T`, wrapped as `cache.ErrSerialization` — rather than reaching `proto.Marshal` and producing an opaque error or corrupting the cache.

**Use cases:**

- Serialize generated proto types for any `ubgo/cache` backend.
- Catch a wiring bug (a non-proto value reached this codec) as a clear typed error in tests.

```go
b, err := codecprotobuf.Codec{}.Encode(&pb.User{Id: 42, Name: "Ada"})
if err != nil { /* ... */ }

_, err = codecprotobuf.Codec{}.Encode("not a proto message")
if errors.Is(err, cache.ErrSerialization) {
	// "protobuf codec needs a proto.Message, got string"
	log.Printf("%v", err)
}
_ = b
```

---

### Codec.Decode

```go
func (Codec) Decode(data []byte, v any) error
```

Unmarshals `data` into a `proto.Message` pointer (`proto.Unmarshal` populates it in place). `v` must be a non-nil `proto.Message`; a non-proto value is asserted up front so a wrong type surfaces as `cache.ErrSerialization` instead of a silent untouched result or a runtime panic.

**Use cases:**

- Round-trip proto values through `cache.Remember` / `cache.GetT`.
- Treat a poisoned/foreign cache entry as a typed error so callers can fall back to the loader.

```go
var u pb.User
if err := codecprotobuf.Codec{}.Decode(b, &u); err != nil {
	if errors.Is(err, cache.ErrSerialization) {
		// corrupt/foreign entry — reload from source
	}
}
```

End-to-end with `cache.Remember`:

```go
ctx := context.Background()
u, err := cache.Remember(ctx, c, "user:42", time.Minute,
	func(ctx context.Context) (*pb.User, error) {
		return &pb.User{Id: 42, Name: "Ada"}, nil
	},
	cache.WithCodec(codecprotobuf.Codec{}),
)
_ = u
_ = err
```

---

### Schema-evolution use case

Protobuf's defining strength: cached bytes survive schema changes within the usual proto compatibility rules.

1. Service v1 caches `pb.User{Id, Name}`.
2. You add `Email string = 3;` to the `.proto` and regenerate.
3. Old cached entries written by v1 still decode under v2 — `Email` is simply the zero value. No cache flush, no `ErrSerialization`.
4. Conversely, a v1 reader decoding a v2-written entry ignores the unknown `Email` field rather than failing.

This is why `codec-protobuf` rejects non-proto values *loudly*: schema-evolution guarantees only hold if every entry is genuinely a `proto.Message`. Silent corruption would defeat the entire reason to choose Protobuf over JSON/Gob/msgpack here.
