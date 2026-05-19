// coverage_test.go — covers Name and the Decode-into-real-proto corrupt-
// bytes path (proto.Unmarshal error wrapped as ErrSerialization).

package codecprotobuf_test

import (
	"errors"
	"testing"

	"github.com/ubgo/cache"
	codecprotobuf "github.com/ubgo/cache/contrib/codec-protobuf"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestProtobufName(t *testing.T) {
	if n := (codecprotobuf.Codec{}).Name(); n != "protobuf" {
		t.Fatalf("Name() = %q, want protobuf", n)
	}
}

func TestProtobufDecodeCorruptBytes(t *testing.T) {
	c := codecprotobuf.Codec{}
	// 0x08 starts a varint field (tag 1, wire type 0) but no value byte
	// follows → proto.Unmarshal fails on a real proto.Message target.
	out := &wrapperspb.StringValue{}
	err := c.Decode([]byte{0x08}, out)
	if err == nil {
		t.Fatal("expected decode error on truncated protobuf")
	}
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("decode error should wrap ErrSerialization, got %v", err)
	}
}

func TestProtobufEncodeNonProto(t *testing.T) {
	// Explicitly drives the non-proto assertion branch in Encode.
	_, err := codecprotobuf.Codec{}.Encode(42)
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("want ErrSerialization for int, got %v", err)
	}
}

func TestProtobufDecodeNonProtoTarget(t *testing.T) {
	var x int
	err := codecprotobuf.Codec{}.Decode([]byte{0x08, 0x01}, &x)
	if !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("want ErrSerialization for non-proto target, got %v", err)
	}
}
