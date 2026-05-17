// protobuf_test.go — tests for the Protocol Buffers codec (contrib/codec-protobuf/protobuf.go).

package codecprotobuf_test

import (
	"errors"
	"testing"

	"github.com/ubgo/cache"
	codecprotobuf "github.com/ubgo/cache/contrib/codec-protobuf"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestProtobufRoundtrip(t *testing.T) {
	c := codecprotobuf.Codec{}
	in := wrapperspb.String("hello-proto")
	b, err := c.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out := &wrapperspb.StringValue{}
	if err := c.Decode(b, out); err != nil {
		t.Fatal(err)
	}
	if out.GetValue() != "hello-proto" {
		t.Fatalf("roundtrip mismatch: %q", out.GetValue())
	}
}

func TestProtobufStructValue(t *testing.T) {
	c := codecprotobuf.Codec{}
	in, err := structpb.NewStruct(map[string]any{"n": 1.0, "s": "x"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.Encode(in)
	out := &structpb.Struct{}
	if err := c.Decode(b, out); err != nil {
		t.Fatal(err)
	}
	if out.GetFields()["s"].GetStringValue() != "x" {
		t.Fatalf("struct roundtrip wrong: %v", out.AsMap())
	}
}

func TestProtobufRejectsNonProto(t *testing.T) {
	c := codecprotobuf.Codec{}
	if _, err := c.Encode("not a proto"); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("want ErrSerialization, got %v", err)
	}
	var s string
	if err := c.Decode([]byte{0x08}, &s); !errors.Is(err, cache.ErrSerialization) {
		t.Fatalf("want ErrSerialization on non-proto target, got %v", err)
	}
}
