# codec-msgpack — feature cookbook

Every exported identifier in `github.com/ubgo/cache/contrib/codec-msgpack`, with concrete use cases and runnable snippets.

Import paths used throughout:

```go
import (
    "github.com/ubgo/cache"
    codecmsgpack "github.com/ubgo/cache/contrib/codec-msgpack"
)
```

---

### Codec

```go
type Codec struct{}
```

A zero-field value type implementing `cache.Codec` using MessagePack. Construct it with a literal `codecmsgpack.Codec{}` — no constructor, no configuration, safe to share/copy and use concurrently.

**Use cases:**

- Replace the default JSON codec with a smaller, faster binary encoding for all cached values.
- Shrink Redis memory and network for large struct-heavy cache entries.
- Keep cross-language readability (Python/Ruby/JS can read the same bytes) — unlike Gob.

```go
backend := cachetest.NewMock()
c := cache.NewTyped[User](backend, cache.WithCodec(codecmsgpack.Codec{}))
_ = c
```

---

### Codec.Name

```go
func (Codec) Name() string // returns "msgpack"
```

Returns the stable codec identifier `"msgpack"`.

**Use cases:**

- Log/metric label for which codec a cache is using.
- Guard rails: assert the expected codec is wired before serving traffic.
- Compose into a wrapper codec's name (e.g. `codec-zstd` reports `"zstd+msgpack"`).

```go
fmt.Println(codecmsgpack.Codec{}.Name()) // msgpack
```

---

### Codec.Encode

```go
func (Codec) Encode(v any) ([]byte, error)
```

Marshals `v` to MessagePack. On failure the error is wrapped so `errors.Is(err, cache.ErrSerialization)` holds; the underlying msgpack error is rendered with `%v` (message only) so callers stay decoupled from the msgpack library's error types.

**Use cases:**

- Serialize values for any `ubgo/cache` backend via `cache.WithCodec`.
- Detect un-encodable values (e.g. channels, funcs) as a recognizable `cache.ErrSerialization` rather than an opaque library error.

```go
b, err := codecmsgpack.Codec{}.Encode(User{ID: 42, Name: "Ada"})
if errors.Is(err, cache.ErrSerialization) {
	log.Printf("not encodable: %v", err)
}
_ = b
```

---

### Codec.Decode

```go
func (Codec) Decode(data []byte, v any) error
```

Unmarshals MessagePack `data` into `v` (a pointer). Corrupt or foreign-format bytes surface as `cache.ErrSerialization` (same wrapping rationale as `Encode`), so a poisoned cache entry is a recognizable, non-panicking failure.

**Use cases:**

- Round-trip cached values through `cache.Remember` / `cache.GetT`.
- Treat a poisoned/legacy cache entry as a typed error so callers can fall back to the loader instead of crashing.

```go
var u User
if err := codecmsgpack.Codec{}.Decode(b, &u); err != nil {
	if errors.Is(err, cache.ErrSerialization) {
		// poisoned entry — reload from source
	}
}
```

End-to-end with `cache.Remember`:

```go
ctx := context.Background()
u, err := cache.Remember(ctx, c, "user:42", time.Minute,
	func(ctx context.Context) (User, error) {
		return User{ID: 42, Name: "Ada"}, nil
	},
	cache.WithCodec(codecmsgpack.Codec{}),
)
_ = u
_ = err
```

---

### vs JSON / Gob

| | msgpack (this) | JSON (core default) | Gob |
|---|---|---|---|
| Size | small (binary) | largest (text) | small (binary) |
| Speed | fast | slower | fast |
| Cross-language | yes | yes | **no** (Go-only) |
| Human-debuggable bytes | no | **yes** | no |
| Schema evolution | tolerant (map-based) | tolerant | brittle |

Pick msgpack when you want JSON-like portability with binary-size/speed wins. Stay on JSON when you need to eyeball raw cached bytes in `redis-cli`. Use protobuf (`codec-protobuf`) when you need strict schema evolution across services.
