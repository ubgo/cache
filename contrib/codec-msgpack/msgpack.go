// msgpack.go — MessagePack cache.Codec implementation (package codecmsgpack, github.com/ubgo/cache/contrib/codec-msgpack).
//
// Package role: a standalone contrib MODULE (its own go.mod) keeping the
// msgpack dependency out of the dependency-free core; the next comment
// block is its canonical package doc (blank-line-separated so this header
// is not a duplicate package comment).
//
// This file: the entire module — Codec implementing cache.Codec
// (Encode/Decode/Name) via vmihailenco/msgpack, used through
// cache.WithCodec(codecmsgpack.Codec{}). The WHY: more compact + faster
// than JSON while still cross-language. Invariant: encode/decode failures
// are wrapped so errors.Is(err, cache.ErrSerialization) holds, and the
// underlying library error is rendered with %v (message only) so callers
// stay decoupled from msgpack's internal error types.
//
// AI-context: implements codec.go's Codec contract from a sibling module —
// the wrapped-error / Name() conventions must match the built-in codecs.

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
