package cachetest

import (
	"context"
	"math"
	"math/rand"

	"github.com/ubgo/cache"
)

// Zipfian generates keys following a Zipf distribution (a few very hot keys,
// a long cold tail) — the shape of almost all real cache traffic. s > 1
// skews harder toward the hot set.
type Zipfian struct {
	z *rand.Zipf
}

// NewZipfian builds a generator over n distinct keys with skew s (try 1.07)
// seeded by seed for reproducibility.
func NewZipfian(n int, s float64, seed int64) *Zipfian {
	r := rand.New(rand.NewSource(seed))
	// rand.NewZipf requires s > 1; v >= 1.
	z := rand.NewZipf(r, s, 1, uint64(n-1))
	return &Zipfian{z: z}
}

// Key returns the next key, e.g. "k:000123".
func (z *Zipfian) Key() string {
	v := z.z.Uint64()
	return "k:" + pad(v)
}

func pad(v uint64) string {
	const w = 8
	b := make([]byte, w)
	for i := w - 1; i >= 0; i-- {
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b)
}

// HitRate replays ops accesses against c using a read-through pattern: on a
// miss the "loader" stores the key, so the result measures how well the
// eviction policy retains the hot set under capacity pressure. Returns the
// observed hit ratio in [0,1].
func HitRate(c cache.Cache, gen func() string, ops int) float64 {
	ctx := context.Background()
	var hits, total int
	val := []byte("v")
	for i := 0; i < ops; i++ {
		k := gen()
		total++
		if _, err := c.Get(ctx, k); err == nil {
			hits++
			continue
		}
		_ = c.Set(ctx, k, val, 0) // read-through fill
	}
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// ScanGen returns a generator that interleaves a small repeatedly-accessed hot
// set with a long stream of unique one-shot "scan" keys — the classic pattern
// that flushes a naive LRU. hotEvery=1-in-N accesses hit the hot set.
func ScanGen(hotSize, hotEvery int, seed int64) func() string {
	r := rand.New(rand.NewSource(seed))
	scan := 0
	return func() string {
		if r.Intn(hotEvery) == 0 {
			return "hot:" + pad(uint64(r.Intn(hotSize)))
		}
		scan++
		return "scan:" + pad(uint64(scan))
	}
}

// Round rounds x to 4 decimals (stable benchmark reporting).
func Round(x float64) float64 { return math.Round(x*1e4) / 1e4 }
