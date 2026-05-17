// zstd_test.go — tests for the size-thresholded zstd codec (contrib/codec-zstd/zstd.go).

package codeczstd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ubgo/cache"
	codeczstd "github.com/ubgo/cache/contrib/codec-zstd"
)

func TestZstdSmallStaysRaw(t *testing.T) {
	c := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(1024))
	enc, err := c.Encode(map[string]int{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if enc[0] != 0x00 { // hdrRaw
		t.Fatalf("small payload should be stored raw, header=0x%02x", enc[0])
	}
	var out map[string]int
	if err := c.Decode(enc, &out); err != nil || out["n"] != 1 {
		t.Fatalf("roundtrip: %v %v", out, err)
	}
}

func TestZstdLargeCompresses(t *testing.T) {
	c := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(64))
	big := strings.Repeat("highly compressible ", 500)
	enc, err := c.Encode(big)
	if err != nil {
		t.Fatal(err)
	}
	if enc[0] != 0x01 { // hdrZstd
		t.Fatalf("large payload should be compressed, header=0x%02x", enc[0])
	}
	raw, _ := cache.JSONCodec{}.Encode(big)
	if len(enc) >= len(raw) {
		t.Fatalf("compressed (%d) not smaller than raw (%d)", len(enc), len(raw))
	}
	var out string
	if err := c.Decode(enc, &out); err != nil || out != big {
		t.Fatalf("roundtrip mismatch (err=%v)", err)
	}
}

func TestZstdRejectsCorruptHeader(t *testing.T) {
	c := codeczstd.New(cache.JSONCodec{})
	var s string
	if err := c.Decode([]byte{0x09, 0x01, 0x02}, &s); err == nil {
		t.Fatal("expected error on unknown header")
	}
	if err := c.Decode(nil, &s); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestZstdCorruptBodyDetected(t *testing.T) {
	c := codeczstd.New(cache.JSONCodec{}, codeczstd.WithMinBytes(1))
	enc, _ := c.Encode(strings.Repeat("x", 200))
	enc = append(enc[:len(enc)-3], bytes.Repeat([]byte{0xff}, 3)...)
	var s string
	if err := c.Decode(enc, &s); err == nil {
		t.Fatal("expected zstd decode error on corrupt body")
	}
}
