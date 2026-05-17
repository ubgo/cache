# Admin HTTP endpoint — `github.com/ubgo/cache/admin`

A small, dependency-free HTTP surface for inspecting a `cache.Cache` in
production: stats, single-key inspection, and an auth-gated manual evict. It
imports only `net/http` + `encoding/json`.

```go
import (
	"net/http"
	"os"

	"github.com/ubgo/cache"
	"github.com/ubgo/cache/admin"
	mem "github.com/ubgo/cache-mem"
)
```

---

### `admin.Options`

```go
type Options struct {
	Prefix     string                       // route prefix (default "/cache")
	Authorized func(r *http.Request) bool    // gates the evict route; nil ⇒ evict always 403
}
```

Safe by default: if `Authorized` is nil, the mutating route always returns
`403` and key values are never exposed unless explicitly requested.

### `admin.Mount(mux *http.ServeMux, c cache.Cache, opts Options)`

One-line: register the admin routes on an existing `*http.ServeMux`.

Use cases: bolt cache introspection onto an existing internal admin mux; expose
stats to a scraper.

```go
mux := http.NewServeMux()
admin.Mount(mux, mem.New(), admin.Options{
	Prefix: "/cache",
	Authorized: func(r *http.Request) bool {
		return r.Header.Get("X-Admin-Token") == os.Getenv("ADMIN_TOKEN")
	},
})
http.ListenAndServe(":8080", mux)
```

### `admin.Handler(c cache.Cache, opts Options) http.Handler`

One-line: a standalone `http.Handler` with the routes mounted (when you want
the admin surface on its own listener / sub-router).

Use cases: serve the admin API on a separate internal-only port.

```go
go http.ListenAndServe("127.0.0.1:9000", admin.Handler(c, admin.Options{
	Authorized: func(r *http.Request) bool { return internalNetwork(r) },
}))
```

---

## Routes (Prefix default `/cache`)

| Method | Route | Returns |
|---|---|---|
| `GET`  | `/cache/stats` | JSON of `cache.Stats` + `hit_ratio` |
| `GET`  | `/cache/key?key=foo` | `{found, ttl_ms, bytes}`; add `&value=1` for base64 `value_b64` |
| `POST` | `/cache/evict?key=foo` | deletes one key — **requires `Authorized`** (else `403`) |

`ttl_ms` is `0` for a no-expiry/unknown key. Key values may contain PII, so the
value is omitted unless `&value=1` is explicitly passed, and `evict` is refused
unless `Options.Authorized` returns true. Non-GET on read routes → `405`;
missing `key` → `400`; absent key on `/key` → `404 {"found":false}`.

```sh
curl -s localhost:8080/cache/stats
# {"hits":12,"misses":3,...,"hit_ratio":0.8}

curl -s 'localhost:8080/cache/key?key=user:42'
# {"found":true,"bytes":128,"ttl_ms":54000}

curl -s 'localhost:8080/cache/key?key=user:42&value=1'
# {"found":true,"bytes":128,"ttl_ms":54000,"value_b64":"…"}

curl -s -X POST -H 'X-Admin-Token: s3cret' \
  'localhost:8080/cache/evict?key=user:42'
# {"evicted":"user:42"}

curl -s -X POST 'localhost:8080/cache/evict?key=user:42'   # no token
# 403 {"error":"not authorized"}
```

Tip: mount on a [`Namespaced`](./namespacing.md) or
[`Instrument`](./observability.md)ed cache and the stats/evict operate through
that same view.
