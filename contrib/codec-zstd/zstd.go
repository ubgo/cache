// Package codeczstd wraps any cache.Codec with size-thresholded zstd
// compression. Small values are stored uncompressed (compression would be a
// net loss); large values are shrunk. A 1-byte header records which path was
// taken so Decode is unambiguous. Kept out of core (klauspost/compress dep).
//
//	codec := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(1<<14))
//	v, _ := cache.Remember(ctx, c, key, ttl, load, cache.WithCodec(codec))
package codeczstd

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
	"github.com/ubgo/cache"
)

// Framing header values. These are a PERSISTED on-disk format: the first
// byte of every stored value records which path Encode took, so Decode is
// unambiguous and threshold-independent. Changing these constants would make
// every previously cached entry undecodable — treat them as frozen.
const (
	hdrRaw  = 0x00 // remaining bytes are raw inner-codec output
	hdrZstd = 0x01 // remaining bytes are a zstd frame of inner-codec output
)

// Codec compresses the inner codec's output with zstd above a size threshold.
type Codec struct {
	inner cache.Codec
	min   int
	enc   *zstd.Encoder
	dec   *zstd.Decoder
}

// Option configures New.
type Option func(*Codec)

// WithMinBytes sets the threshold (encoded bytes) above which zstd is applied.
// Default 16 KiB. Values at or below the threshold are stored uncompressed.
func WithMinBytes(n int) Option { return func(c *Codec) { c.min = n } }

// New wraps inner with zstd compression.
func New(inner cache.Codec, opts ...Option) *Codec {
	if inner == nil {
		inner = cache.DefaultCodec
	}
	// Default threshold 16 KiB; below this, zstd framing overhead + CPU
	// outweigh any size saving, so such values are stored raw.
	c := &Codec{inner: inner, min: 1 << 14}
	for _, o := range opts {
		o(c)
	}
	// nil writer/reader => one-shot EncodeAll/DecodeAll mode (not streaming).
	// The errors are intentionally discarded: NewWriter/NewReader only fail
	// on invalid options and we pass none. The encoder/decoder are created
	// once and reused; klauspost EncodeAll/DecodeAll are concurrency-safe.
	c.enc, _ = zstd.NewWriter(nil)
	c.dec, _ = zstd.NewReader(nil)
	return c
}

// Name returns the codec identifier.
func (c *Codec) Name() string { return "zstd+" + c.inner.Name() }

// Encode serializes via the inner codec, compressing if over the threshold.
func (c *Codec) Encode(v any) ([]byte, error) {
	plain, err := c.inner.Encode(v)
	if err != nil {
		// Inner errors already carry cache.ErrSerialization; pass through
		// unchanged rather than double-wrapping.
		return nil, err
	}
	// Threshold is on the INNER-ENCODED length, not the original value size.
	if len(plain) <= c.min {
		return append([]byte{hdrRaw}, plain...), nil
	}
	// EncodeAll appends the zstd frame to the supplied buffer, so passing
	// {hdrZstd} as the destination yields 0x01 || frame in one call.
	out := c.enc.EncodeAll(plain, []byte{hdrZstd})
	return out, nil
}

// Decode reverses Encode.
func (c *Codec) Decode(data []byte, v any) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: empty zstd payload", cache.ErrSerialization)
	}
	// Dispatch purely on the stored header byte — never on c.min. This is
	// what makes WithMinBytes safe to change after data is written: old
	// entries still decode via the path they were written with.
	hdr, body := data[0], data[1:]
	switch hdr {
	case hdrRaw:
		return c.inner.Decode(body, v)
	case hdrZstd:
		plain, err := c.dec.DecodeAll(body, nil)
		if err != nil {
			return fmt.Errorf("%w: zstd decode: %v", cache.ErrSerialization, err)
		}
		return c.inner.Decode(plain, v)
	default:
		return fmt.Errorf("%w: unknown zstd header 0x%02x", cache.ErrSerialization, hdr)
	}
}

var _ cache.Codec = (*Codec)(nil)
