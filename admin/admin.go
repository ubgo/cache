// admin.go — the dependency-free HTTP inspection surface for a cache.Cache (package admin, github.com/ubgo/cache/admin).
//
// Package role: admin is a sibling sub-package of github.com/ubgo/cache
// providing a production debug/ops HTTP surface; it imports only net/http +
// encoding/json + the core cache module (no third-party deps).
//
// This file: the entire package — Options, Mount/Handler, and the stats/
// key/evict handlers. The WHY: inspect cache health and surgically evict in
// prod without a backend-specific console. Security invariants: a key value
// may contain PII so it is omitted unless &value=1 is explicitly passed; the
// evict route is refused with 403 unless Options.Authorized returns true
// (nil Authorized => always 403, safe by default).
//
// AI-context: the next comment block IS the package doc (separated by a
// blank line so this file header is not a duplicate package comment). Routes
// are method-checked and JSON-only; evict uses context.WithoutCancel so a
// client disconnect cannot abort an in-progress delete.

// Package admin exposes a small, dependency-free HTTP surface for inspecting
// a cache.Cache in production: stats, single-key inspection, and an
// auth-gated manual evict. It imports only net/http + encoding/json.
//
//	mux := http.NewServeMux()
//	admin.Mount(mux, c, admin.Options{
//	    Prefix: "/cache",
//	    Authorized: func(r *http.Request) bool {
//	        return r.Header.Get("X-Admin-Token") == os.Getenv("ADMIN_TOKEN")
//	    },
//	})
//
// Routes (Prefix default "/cache"):
//
//	GET  /cache/stats          → JSON cache.Stats
//	GET  /cache/key?key=foo    → {found,ttl_ms,bytes}; &value=1 adds base64 value
//	POST /cache/evict?key=foo  → deletes one key (requires Authorized)
//
// Key values may contain PII, so the value is omitted unless explicitly
// requested, and evict is refused unless Options.Authorized returns true.
package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ubgo/cache"
)

// Options configures Mount.
type Options struct {
	// Prefix is the route prefix (default "/cache").
	Prefix string
	// Authorized gates mutating routes (evict). If nil, those routes always
	// return 403 — safe by default.
	Authorized func(r *http.Request) bool
}

// Mount registers the admin routes on mux.
func Mount(mux *http.ServeMux, c cache.Cache, opts Options) {
	prefix := opts.Prefix
	if prefix == "" {
		prefix = "/cache"
	}
	h := &handler{c: c, opts: opts}
	mux.HandleFunc(prefix+"/stats", h.stats)
	mux.HandleFunc(prefix+"/key", h.key)
	mux.HandleFunc(prefix+"/evict", h.evict)
}

// Handler returns a standalone http.Handler with the routes mounted.
func Handler(c cache.Cache, opts Options) http.Handler {
	mux := http.NewServeMux()
	Mount(mux, c, opts)
	return mux
}

type handler struct {
	c    cache.Cache
	opts Options
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handler) stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	s := h.c.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"hits":               s.Hits,
		"misses":             s.Misses,
		"sets":               s.Sets,
		"deletes":            s.Deletes,
		"evictions":          s.Evictions,
		"evictions_by_cause": s.EvictionsByCause,
		"entries":            s.Entries,
		"bytes":              s.Bytes,
		"hit_ratio":          s.HitRatio(),
	})
}

func (h *handler) key(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	k := r.URL.Query().Get("key")
	if k == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing key"})
		return
	}
	ctx := r.Context()
	v, err := h.c.Get(ctx, k)
	if errors.Is(err, cache.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"found": false})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ttl, _ := h.c.TTL(ctx, k)
	resp := map[string]any{
		"found":  true,
		"bytes":  len(v),
		"ttl_ms": ttlMS(ttl),
	}
	if r.URL.Query().Get("value") == "1" {
		resp["value_b64"] = base64.StdEncoding.EncodeToString(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

func ttlMS(d time.Duration) int64 {
	if d <= 0 {
		return 0 // 0 = no expiry / unknown
	}
	return d.Milliseconds()
}

func (h *handler) evict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if h.opts.Authorized == nil || !h.opts.Authorized(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized"})
		return
	}
	k := r.URL.Query().Get("key")
	if k == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing key"})
		return
	}
	if err := h.c.Del(context.WithoutCancel(r.Context()), k); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evicted": k})
}
