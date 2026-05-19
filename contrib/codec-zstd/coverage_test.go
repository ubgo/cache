// coverage_test.go — covers New(nil)→DefaultCodec, Name, and inner-codec
// Encode error propagation (passed through unwrapped).

package codeczstd_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ubgo/cache"
	codeczstd "github.com/ubgo/cache/contrib/codec-zstd"
)

func TestZstdNilInnerUsesDefaultCodec(t *testing.T) {
	c := codeczstd.New(nil)
	// Name embeds the inner codec's name; DefaultCodec must be in effect.
	if !strings.HasPrefix(c.Name(), "zstd+") {
		t.Fatalf("Name() = %q, want zstd+ prefix", c.Name())
	}
	if c.Name() != "zstd+"+cache.DefaultCodec.Name() {
		t.Fatalf("nil inner should fall back to DefaultCodec, got %q", c.Name())
	}
	// And it must actually round-trip through the default codec.
	enc, err := c.Encode(map[string]int{"x": 1})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var out map[string]int
	if err := c.Decode(enc, &out); err != nil || out["x"] != 1 {
		t.Fatalf("roundtrip via default codec failed: %v %v", out, err)
	}
}

func TestZstdName(t *testing.T) {
	c := codeczstd.New(cache.JSONCodec{})
	if c.Name() != "zstd+"+(cache.JSONCodec{}).Name() {
		t.Fatalf("Name() = %q", c.Name())
	}
}

// errCodec always fails Encode, to verify the inner error is propagated
// through Codec.Encode unchanged (no double wrapping).
type errCodec struct{}

var errInner = errors.New("inner encode boom")

func (errCodec) Name() string               { return "errc" }
func (errCodec) Encode(any) ([]byte, error) { return nil, errInner }
func (errCodec) Decode([]byte, any) error   { return errInner }

func TestZstdInnerEncodeErrorPropagates(t *testing.T) {
	c := codeczstd.New(errCodec{}, codeczstd.WithMinBytes(0))
	_, err := c.Encode("anything")
	if !errors.Is(err, errInner) {
		t.Fatalf("inner encode error should pass through, got %v", err)
	}
}

func TestZstdRawHeaderDecodesViaInner(t *testing.T) {
	// Explicit 0x00 header path with a known inner codec.
	c := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(1<<20))
	enc, err := c.Encode([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if enc[0] != 0x00 {
		t.Fatalf("expected raw header, got 0x%02x", enc[0])
	}
	var out []int
	if err := c.Decode(enc, &out); err != nil || len(out) != 3 {
		t.Fatalf("raw decode failed: %v %v", out, err)
	}
}
