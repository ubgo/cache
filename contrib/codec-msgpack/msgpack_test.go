// msgpack_test.go — tests for the MessagePack codec (contrib/codec-msgpack/msgpack.go).

package codecmsgpack_test

import (
	"context"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/cachetest"
	codecmsgpack "github.com/ubgo/cache/contrib/codec-msgpack"
)

type person struct {
	Name string
	Age  int
}

func TestMsgpackRoundtripViaRemember(t *testing.T) {
	ctx := context.Background()
	c := cachetest.NewMock()
	want := person{Name: "ada", Age: 36}

	got, err := cache.Remember(ctx, c, "p", time.Minute,
		func(context.Context) (person, error) { return want, nil },
		cache.WithCodec(codecmsgpack.Codec{}))
	if err != nil || got != want {
		t.Fatalf("first: %+v %v", got, err)
	}
	// Second call hits cache and must decode with the same codec.
	got2, err := cache.Remember(ctx, c, "p", time.Minute,
		func(context.Context) (person, error) { t.Fatal("loader should not run"); return person{}, nil },
		cache.WithCodec(codecmsgpack.Codec{}))
	if err != nil || got2 != want {
		t.Fatalf("cached: %+v %v", got2, err)
	}
}

func TestMsgpackIsCompact(t *testing.T) {
	mp, _ := codecmsgpack.Codec{}.Encode(person{Name: "ada", Age: 36})
	js, _ := cache.JSONCodec{}.Encode(person{Name: "ada", Age: 36})
	if len(mp) >= len(js) {
		t.Fatalf("expected msgpack (%d) smaller than json (%d)", len(mp), len(js))
	}
}
