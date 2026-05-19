// coverage_test.go — branch coverage for the admin HTTP surface: wrong-method
// rejections, missing-key, backend errors, custom prefix, ttl_ms positive
// branch, and evict success/missing/backend-error paths. Test-only.

package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ubgo/cache/admin"
	"github.com/ubgo/cache/cachetest"
)

func TestAdminMethodNotAllowed(t *testing.T) {
	srv, _ := newServer(t, true)
	if code := do(t, http.MethodPost, srv.URL+"/cache/stats", "", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("stats POST: %d", code)
	}
	if code := do(t, http.MethodPost, srv.URL+"/cache/key?key=k", "", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("key POST: %d", code)
	}
	if code := do(t, http.MethodGet, srv.URL+"/cache/evict?key=k", "secret", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("evict GET: %d", code)
	}
}

func TestAdminKeyMissingAndError(t *testing.T) {
	// Missing ?key= -> 400.
	srv, _ := newServer(t, false)
	if code := do(t, http.MethodGet, srv.URL+"/cache/key", "", nil); code != http.StatusBadRequest {
		t.Fatalf("missing key: %d", code)
	}

	// Backend Get error (non-NotFound) -> 500.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"get": errors.New("io")}
	s2 := httptest.NewServer(admin.Handler(fc, admin.Options{}))
	t.Cleanup(s2.Close)
	if code := do(t, http.MethodGet, s2.URL+"/cache/key?key=k", "", nil); code != http.StatusInternalServerError {
		t.Fatalf("backend error key: %d", code)
	}
}

func TestAdminKeyTTLPositive(t *testing.T) {
	srv, c := newServer(t, false)
	_ = c.Set(context.Background(), "k", []byte("vvv"), time.Hour)
	var body map[string]any
	do(t, http.MethodGet, srv.URL+"/cache/key?key=k", "", &body)
	if body["ttl_ms"].(float64) <= 0 {
		t.Fatalf("ttl_ms should be positive, got %v", body["ttl_ms"])
	}
	// value omitted when value!=1
	if _, ok := body["value_b64"]; ok {
		t.Fatal("value must be omitted without value=1")
	}
}

func TestAdminKeyNoExpiryTTLZero(t *testing.T) {
	srv, c := newServer(t, false)
	_ = c.Set(context.Background(), "k", []byte("v"), 0) // no expiry
	var body map[string]any
	do(t, http.MethodGet, srv.URL+"/cache/key?key=k", "", &body)
	if body["ttl_ms"].(float64) != 0 {
		t.Fatalf("no-expiry ttl_ms must be 0, got %v", body["ttl_ms"])
	}
}

func TestAdminEvictMissingAndBackendError(t *testing.T) {
	// Authorized but missing key -> 400.
	c := cachetest.NewMock()
	srv := httptest.NewServer(admin.Handler(c, admin.Options{
		Prefix:     "/x",
		Authorized: func(*http.Request) bool { return true },
	}))
	t.Cleanup(srv.Close)
	if code := do(t, http.MethodPost, srv.URL+"/x/evict", "", nil); code != http.StatusBadRequest {
		t.Fatalf("evict missing key: %d", code)
	}
	// Successful evict on a custom prefix.
	_ = c.Set(context.Background(), "k", []byte("v"), time.Hour)
	if code := do(t, http.MethodPost, srv.URL+"/x/evict?key=k", "", nil); code != http.StatusOK {
		t.Fatalf("custom-prefix evict: %d", code)
	}

	// Backend Del error -> 500.
	fc := cachetest.NewMock()
	fc.FailOn = map[string]error{"del": errors.New("boom")}
	s2 := httptest.NewServer(admin.Handler(fc, admin.Options{
		Authorized: func(*http.Request) bool { return true },
	}))
	t.Cleanup(s2.Close)
	if code := do(t, http.MethodPost, s2.URL+"/cache/evict?key=k", "", nil); code != http.StatusInternalServerError {
		t.Fatalf("evict backend error: %d", code)
	}
}

func TestAdminMountDefaultPrefixAndOptions(t *testing.T) {
	// Mount (not Handler) with default prefix.
	mux := http.NewServeMux()
	c := cachetest.NewMock()
	admin.Mount(mux, c, admin.Options{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if code := do(t, http.MethodGet, srv.URL+"/cache/stats", "", nil); code != http.StatusOK {
		t.Fatalf("default-prefix stats: %d", code)
	}
}
