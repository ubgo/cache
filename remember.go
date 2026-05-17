// remember.go — the typed load-through workhorse + all caching-pattern options (package cache, github.com/ubgo/cache).
//
// Package role: cache is the root bytes-level cache contract of the
// ubgo/cache family; see doc.go for the package overview.
//
// This file: implements Remember (cache-or-single-flight-load-and-store)
// plus GetT/SetT, every RememberOpt (refresh-ahead, stale-while-revalidate,
// stale-if-error, negative caching, jitter, codec), and the internal
// rememberCfg / envelope. The WHY: composes all the production caching
// patterns over the bytes Cache. Wire-format invariant: Remember stores a
// JSON-encoded `envelope{Data,Born,Soft,Missing}` so metadata survives any
// value codec; GetT/SetT use the PLAIN codec (no envelope) — pair reads and
// writes with the matching function or the envelope won't decode.
//
// AI-context: Remember dedups loads via singleflight.go's loaderFlight; a
// stored entry's hard backend TTL = soft TTL + widest stale window so the
// stale-* options can still read an expired-but-present value.

package cache

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"time"
)

// LoadFn computes a value on cache miss (or background refresh).
type LoadFn[T any] func(ctx context.Context) (T, error)

// rememberCfg is the resolved option set for one Remember call.
type rememberCfg struct {
	codec          Codec
	refreshAhead   float64       // 0..1 fraction of TTL; 0 disables
	staleWindow    time.Duration // serve-stale-and-revalidate window past hard TTL
	staleIfError   time.Duration // serve stale this long past hard TTL if loader errors
	negativeTTL    time.Duration // cache ErrNotFound this long; 0 disables
	jitter         float64       // 0..1 +/- fraction applied to stored TTL
	clock          func() time.Time
	asyncRefreshFn func(func()) // how background refreshes are launched (test seam)
}

// RememberOpt configures Remember.
type RememberOpt func(*rememberCfg)

// WithCodec overrides the codec used to (de)serialize the value.
func WithCodec(c Codec) RememberOpt { return func(o *rememberCfg) { o.codec = c } }

// WithRefreshAhead refreshes in the background once frac (0..1) of the TTL has
// elapsed, so a hot key never expires under load. The stale-but-valid value is
// returned immediately to the caller that triggers the refresh.
func WithRefreshAhead(frac float64) RememberOpt {
	return func(o *rememberCfg) { o.refreshAhead = frac }
}

// WithStaleWhileRevalidate keeps serving the expired value for d after hard
// expiry while a single background load refreshes it.
func WithStaleWhileRevalidate(d time.Duration) RememberOpt {
	return func(o *rememberCfg) { o.staleWindow = d }
}

// WithStaleIfError serves the expired value for d past hard expiry if (and
// only if) the loader fails — trading staleness for availability.
func WithStaleIfError(d time.Duration) RememberOpt {
	return func(o *rememberCfg) { o.staleIfError = d }
}

// WithNegativeTTL caches a loader's ErrNotFound for d so a missing key does not
// re-run an expensive lookup on every request.
func WithNegativeTTL(d time.Duration) RememberOpt {
	return func(o *rememberCfg) { o.negativeTTL = d }
}

// WithJitter applies +/- frac random noise to the stored TTL so a batch of
// keys written together does not all expire in the same instant.
func WithJitter(frac float64) RememberOpt { return func(o *rememberCfg) { o.jitter = frac } }

func newRememberCfg(opts []RememberOpt) rememberCfg {
	cfg := rememberCfg{
		codec:          DefaultCodec,
		clock:          time.Now,
		asyncRefreshFn: func(f func()) { go f() },
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.codec == nil {
		cfg.codec = DefaultCodec
	}
	return cfg
}

// envelope wraps a stored value with the timestamps the loading patterns need.
// It is JSON-encoded so the metadata survives any value codec.
type envelope struct {
	Data    []byte    `json:"d"`           // value bytes from cfg.codec
	Born    time.Time `json:"b"`           // when written
	Soft    time.Time `json:"s"`           // logical expiry (caller TTL)
	Missing bool      `json:"m,omitempty"` // negative-cache sentinel
}

func (c rememberCfg) applyJitter(ttl time.Duration) time.Duration {
	if c.jitter <= 0 || ttl <= 0 {
		return ttl
	}
	delta := (rand.Float64()*2 - 1) * c.jitter // [-jitter, +jitter]
	j := time.Duration(float64(ttl) * (1 + delta))
	if j < 0 {
		return ttl
	}
	return j
}

// hardTTL is how long the envelope physically lives in the backend: the soft
// TTL plus the widest stale window so stale-* options can still read it.
func (c rememberCfg) hardTTL(soft time.Duration) time.Duration {
	if soft <= 0 {
		return 0 // permanent
	}
	extra := c.staleWindow
	if c.staleIfError > extra {
		extra = c.staleIfError
	}
	return soft + extra
}

// GetT reads and decodes a typed value. Returns ErrNotFound on miss.
func GetT[T any](ctx context.Context, c Cache, key string, opts ...RememberOpt) (T, error) {
	cfg := newRememberCfg(opts)
	var zero T
	raw, err := c.Get(ctx, key)
	if err != nil {
		return zero, err
	}
	env, ok := decodeEnvelope(raw)
	if !ok {
		// Not an envelope (e.g. written by SetT) — plain codec payload.
		var v T
		if derr := cfg.codec.Decode(raw, &v); derr != nil {
			return zero, derr
		}
		return v, nil
	}
	if env.Missing {
		return zero, ErrNotFound
	}
	var v T
	if err := cfg.codec.Decode(env.Data, &v); err != nil {
		return zero, err
	}
	return v, nil
}

// SetT encodes and stores a typed value (no envelope; pair with GetT).
func SetT[T any](ctx context.Context, c Cache, key string, v T, ttl time.Duration, opts ...RememberOpt) error {
	cfg := newRememberCfg(opts)
	b, err := cfg.codec.Encode(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, cfg.applyJitter(ttl))
}

// Remember is the workhorse: return the cached value, or single-flight fn on
// miss and store it. Composes refresh-ahead, stale-while-revalidate,
// stale-if-error, negative caching and TTL jitter.
func Remember[T any](ctx context.Context, c Cache, key string, ttl time.Duration, fn LoadFn[T], opts ...RememberOpt) (T, error) {
	cfg := newRememberCfg(opts)
	var zero T

	if raw, err := c.Get(ctx, key); err == nil {
		if env, ok := decodeEnvelope(raw); ok {
			now := cfg.clock()
			if env.Missing {
				if now.Before(env.Soft) {
					return zero, ErrNotFound
				}
			} else {
				v, decErr := decodeT[T](cfg, env.Data)
				if decErr == nil {
					switch {
					case now.Before(env.Soft):
						// Fresh. Refresh-ahead if past the threshold.
						if cfg.refreshAhead > 0 {
							elapsed := now.Sub(env.Born)
							life := env.Soft.Sub(env.Born)
							if life > 0 && float64(elapsed)/float64(life) >= cfg.refreshAhead {
								flightRefresh(ctx, c, key, ttl, fn, cfg)
							}
						}
						return v, nil
					case cfg.staleWindow > 0 && now.Before(env.Soft.Add(cfg.staleWindow)):
						// Stale-while-revalidate: serve stale, refresh in bg.
						flightRefresh(ctx, c, key, ttl, fn, cfg)
						return v, nil
					}
				}
			}
		}
	} else if !errors.Is(err, ErrNotFound) {
		return zero, err
	}

	// Miss (or expired beyond SWR): single-flight the loader.
	res, _, err := loaderFlight(c).Do(key, func() (any, error) {
		return loadAndStore(ctx, c, key, ttl, fn, cfg)
	})
	if err != nil {
		// Stale-if-error: fall back to a still-readable stale value.
		if cfg.staleIfError > 0 {
			if raw, gerr := c.Get(ctx, key); gerr == nil {
				if env, ok := decodeEnvelope(raw); ok && !env.Missing {
					if v, derr := decodeT[T](cfg, env.Data); derr == nil &&
						cfg.clock().Before(env.Soft.Add(cfg.staleIfError)) {
						return v, nil
					}
				}
			}
		}
		if errors.Is(err, ErrNotFound) {
			return zero, ErrNotFound
		}
		return zero, err
	}
	return res.(T), nil
}

func decodeEnvelope(raw []byte) (envelope, bool) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return envelope{}, false
	}
	if env.Born.IsZero() {
		return envelope{}, false
	}
	return env, true
}

func decodeT[T any](cfg rememberCfg, data []byte) (T, error) {
	var v T
	err := cfg.codec.Decode(data, &v)
	return v, err
}

func loadAndStore[T any](ctx context.Context, c Cache, key string, ttl time.Duration, fn LoadFn[T], cfg rememberCfg) (T, error) {
	var zero T
	v, err := fn(ctx)
	now := cfg.clock()
	if err != nil {
		if errors.Is(err, ErrNotFound) && cfg.negativeTTL > 0 {
			env := envelope{Born: now, Soft: now.Add(cfg.negativeTTL), Missing: true}
			if b, merr := json.Marshal(env); merr == nil {
				_ = c.Set(ctx, key, b, cfg.applyJitter(cfg.negativeTTL))
			}
		}
		return zero, err
	}
	data, err := cfg.codec.Encode(v)
	if err != nil {
		return zero, err
	}
	env := envelope{Data: data, Born: now}
	if ttl > 0 {
		env.Soft = now.Add(ttl)
	} else {
		env.Soft = now.AddDate(100, 0, 0) // effectively never
	}
	if b, merr := json.Marshal(env); merr == nil {
		_ = c.Set(ctx, key, b, cfg.applyJitter(cfg.hardTTL(ttl)))
	}
	return v, nil
}

// flightRefresh fires a single background reload (deduped per key).
func flightRefresh[T any](ctx context.Context, c Cache, key string, ttl time.Duration, fn LoadFn[T], cfg rememberCfg) {
	cfg.asyncRefreshFn(func() {
		bg := context.WithoutCancel(ctx)
		_, _, _ = loaderFlight(c).Do(key, func() (any, error) {
			return loadAndStore(bg, c, key, ttl, fn, cfg)
		})
	})
}

// loaderFlight returns a per-cache single-flight group so concurrent Remember
// calls for the same key load exactly once.
func loaderFlight(c Cache) *flightGroup {
	flightRegMu.Lock()
	defer flightRegMu.Unlock()
	if flightReg == nil {
		flightReg = make(map[Cache]*flightGroup)
	}
	g, ok := flightReg[c]
	if !ok {
		g = &flightGroup{}
		flightReg[c] = g
	}
	return g
}
