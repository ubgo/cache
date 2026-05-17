// Package codecmsgpack provides a MessagePack cache.Codec: more compact and
// faster than JSON, still cross-language. Kept out of core so the core stays
// dependency-free.
//
//	u, err := cache.Remember(ctx, c, key, ttl, load,
//	    cache.WithCodec(codecmsgpack.Codec{}))
package codecmsgpack

import (
	"fmt"

	"github.com/ubgo/cache"
	"github.com/vmihailenco/msgpack/v5"
)

// Codec implements cache.Codec using MessagePack.
type Codec struct{}

// Name returns the codec identifier.
func (Codec) Name() string { return "msgpack" }

// Encode marshals v to MessagePack. A failure is wrapped so that
// errors.Is(err, cache.ErrSerialization) holds; the underlying msgpack error
// is rendered with %v (message only) on purpose, so callers stay decoupled
// from the msgpack library's internal error types.
func (Codec) Encode(v any) ([]byte, error) {
	b, err := msgpack.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: msgpack encode: %v", cache.ErrSerialization, err)
	}
	return b, nil
}

// Decode unmarshals MessagePack into v. Corrupt or foreign-format bytes
// surface as cache.ErrSerialization (same wrapping rationale as Encode), so a
// poisoned cache entry is a recognizable, non-panicking failure.
func (Codec) Decode(data []byte, v any) error {
	if err := msgpack.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: msgpack decode: %v", cache.ErrSerialization, err)
	}
	return nil
}

var _ cache.Codec = Codec{}
