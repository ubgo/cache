package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/admin"
	"github.com/ubgo/cache/cachetest"
)

func newServer(t *testing.T, authed bool) (*httptest.Server, cache.Cache) {
	t.Helper()
	c := cachetest.NewMock()
	opts := admin.Options{}
	if authed {
		opts.Authorized = func(r *http.Request) bool {
			return r.Header.Get("X-Admin-Token") == "secret"
		}
	}
	srv := httptest.NewServer(admin.Handler(c, opts))
	t.Cleanup(srv.Close)
	return srv, c
}

// do issues req, fails the test on transport error, decodes the JSON body (if
// out != nil) and always closes the body. Returns the status code.
func do(t *testing.T, method, url, token string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
	}
	return resp.StatusCode
}

func TestStatsEndpoint(t *testing.T) {
	srv, c := newServer(t, false)
	_ = c.Set(context.Background(), "k", []byte("v"), time.Minute)
	_, _ = c.Get(context.Background(), "k")

	var body map[string]any
	if code := do(t, http.MethodGet, srv.URL+"/cache/stats", "", &body); code != 200 {
		t.Fatalf("status %d", code)
	}
	if body["entries"].(float64) != 1 {
		t.Fatalf("entries: %v", body["entries"])
	}
}

func TestKeyInspect(t *testing.T) {
	srv, c := newServer(t, false)
	_ = c.Set(context.Background(), "k", []byte("hello"), time.Hour)

	var body map[string]any
	do(t, http.MethodGet, srv.URL+"/cache/key?key=k&value=1", "", &body)
	if body["found"] != true || body["bytes"].(float64) != 5 {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["value_b64"] != "aGVsbG8=" {
		t.Fatalf("value_b64: %v", body["value_b64"])
	}

	if code := do(t, http.MethodGet, srv.URL+"/cache/key?key=missing", "", nil); code != http.StatusNotFound {
		t.Fatalf("missing key status: %d", code)
	}
}

func TestEvictRequiresAuth(t *testing.T) {
	srv, c := newServer(t, true)
	_ = c.Set(context.Background(), "k", []byte("v"), time.Hour)

	if code := do(t, http.MethodPost, srv.URL+"/cache/evict?key=k", "", nil); code != http.StatusForbidden {
		t.Fatalf("want 403 without token, got %d", code)
	}
	if ok, _ := c.Has(context.Background(), "k"); !ok {
		t.Fatal("key wrongly evicted without auth")
	}

	if code := do(t, http.MethodPost, srv.URL+"/cache/evict?key=k", "secret", nil); code != 200 {
		t.Fatalf("want 200 with token, got %d", code)
	}
	if ok, _ := c.Has(context.Background(), "k"); ok {
		t.Fatal("key not evicted despite auth")
	}
}

func TestEvictForbiddenWhenNoAuthorizer(t *testing.T) {
	srv, _ := newServer(t, false) // Authorized nil → always 403
	if code := do(t, http.MethodPost, srv.URL+"/cache/evict?key=k", "", nil); code != http.StatusForbidden {
		t.Fatalf("evict must be 403 when no Authorized configured, got %d", code)
	}
}
