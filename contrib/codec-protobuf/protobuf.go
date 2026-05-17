// Package codecprotobuf provides a Protocol Buffers cache.Codec for
// proto.Message values — schema-evolvable, compact, and interoperable with
// other services. Kept out of core so the core stays dependency-free.
//
//	v, err := cache.Remember(ctx, c, key, ttl, load,
//	    cache.WithCodec(codecprotobuf.Codec{}))
//
// Encode requires a proto.Message; Decode requires a non-nil proto.Message
// pointer. Non-proto values fail with cache.ErrSerialization rather than
// silently corrupting data.
package codecprotobuf

import (
	"fmt"

	"github.com/ubgo/cache"
	"google.golang.org/protobuf/proto"
)

// Codec implements cache.Codec using Protocol Buffers wire format.
type Codec struct{}

// Name returns the codec identifier.
func (Codec) Name() string { return "protobuf" }

// Encode marshals a proto.Message. The cache.Codec interface accepts any, so
// the proto requirement can only be enforced at runtime: assert first and
// fail loudly with the offending %T rather than letting a non-proto value
// reach proto.Marshal and produce an opaque error (or corrupt the cache).
func (Codec) Encode(v any) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("%w: protobuf codec needs a proto.Message, got %T", cache.ErrSerialization, v)
	}
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("%w: protobuf encode: %v", cache.ErrSerialization, err)
	}
	return b, nil
}

// Decode unmarshals into a proto.Message pointer. v must be a non-nil
// proto.Message pointer (proto.Unmarshal populates it in place); a non-proto
// value is asserted up front so a wrong type surfaces as ErrSerialization
// instead of a silent untouched result or a runtime panic.
func (Codec) Decode(data []byte, v any) error {
	m, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("%w: protobuf codec needs a proto.Message, got %T", cache.ErrSerialization, v)
	}
	if err := proto.Unmarshal(data, m); err != nil {
		return fmt.Errorf("%w: protobuf decode: %v", cache.ErrSerialization, err)
	}
	return nil
}

var _ cache.Codec = Codec{}
