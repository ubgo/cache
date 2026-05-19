// coverage_test.go — covers Name, the Encode error path (unmarshalable
// type: a chan), and the Decode error path (corrupt bytes), asserting both
// wrap cache.ErrSerialization.

package codecmsgpack_test

import (
	"errors"
	"testing"

	"github.com/ubgo/cache"
	codecmsgpack "github.com/ubgo/cache/contrib/codec-msgpack"
)

func TestMsgpackName(t *testing.T) {
	if n := (codecmsgpack.Codec{}).Name(); n != "msgpack" {
		t.Fatalf("Name() = %q, want msgpack", n)
	}
}

func TestMsgpackRoundtrip(t *testing.T) {
	c := codecmsgpack.Codec{}
	type rec struct {
		A string
		B int
	}
	in := rec{A: "x", B: 7}
	b, err := c.Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var out rec
	if err := c.Decode(b, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
	}
}

func TestMsgpackEncodeError(t *testing.T) {
	// channels are not encodable by msgpack.
	_, err := codecmsgpack.Codec{}.Encode(make(chan int))
	if err == nil {
		t.Fatal("expected encode error for chan")
	}
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("encode error should wrap ErrSerialization, got %v", err)
	}
}

func TestMsgpackDecodeError(t *testing.T) {
	// 0xc1 is the msgpack "never used" byte → guaranteed decode failure.
	var out map[string]any
	err := codecmsgpack.Codec{}.Decode([]byte{0xc1}, &out)
	if err == nil {
		t.Fatal("expected decode error for corrupt bytes")
	}
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("decode error should wrap ErrSerialization, got %v", err)
	}
}
