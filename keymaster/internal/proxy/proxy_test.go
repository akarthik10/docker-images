package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akarthik10/keymaster/internal/grants"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/store"
)

func setup(t *testing.T, upstream string) (*httptest.Server, string, *grants.Service) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tok"), []byte("secret-upstream\n"), 0o600)
	p, err := policy.Parse([]byte(`
agents: [{sa: default/a, allowed_connectors: [up, echo]}]
connectors:
  up:
    kind: proxy
    upstream: ` + upstream + `
    auth: {type: header, name: X-Auth-Token, value_file: tok}
    scopes:
      read:  {tier: 0, max_ttl: 1h, rules: [{methods: [GET, COPY], paths: ["^/v1/.*$"]}]}
  echo:
    kind: proxy
    upstream: "internal:echo"
    scopes:
      any: {tier: 0, max_ttl: 1h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.Open(filepath.Join(dir, "k.db"))
	t.Cleanup(func() { st.Close() })
	g := grants.New(st, p)
	g.Now = time.Now
	px := &Proxy{G: g, CredsDir: dir}
	mux := http.NewServeMux()
	mux.Handle("/p/{connector}/", px.Handler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gr, err := g.Request(context.Background(), "default/a", []string{"up:read", "echo:any"}, "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, d, _ := g.Wait(context.Background(), "default/a", gr.ID, 0)
	return srv, d.Token, g
}

func do(t *testing.T, method, url, tok string, hdr map[string]string) *http.Response {
	req, _ := http.NewRequest(method, url, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestProxy(t *testing.T) {
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.URL.Path == "/v1/redir" {
			http.Redirect(w, r, "http://evil.example/x", 302)
			return
		}
		w.Write([]byte("hi"))
	}))
	defer up.Close()
	srv, tok, g := setup(t, up.URL)

	// no/garbage token -> 401
	if c := do(t, "GET", srv.URL+"/p/up/v1/x", "", nil).StatusCode; c != 401 {
		t.Errorf("no token: %d", c)
	}
	if c := do(t, "GET", srv.URL+"/p/up/v1/x", "nope", nil).StatusCode; c != 401 {
		t.Errorf("bad token: %d", c)
	}
	// allowed, and agent-supplied creds never reach upstream
	r := do(t, "GET", srv.URL+"/p/up/v1/x", tok, map[string]string{"Cookie": "a=b", "X-Auth-Token": "agent-forged"})
	if r.StatusCode != 200 {
		t.Fatalf("allowed: %d", r.StatusCode)
	}
	if got.Get("Authorization") != "" || got.Get("Cookie") != "" || got.Get("X-Auth-Token") != "secret-upstream" {
		t.Errorf("headers leaked/not injected: %v", got)
	}
	// method not allowed, path outside rule, traversal
	if c := do(t, "PUT", srv.URL+"/p/up/v1/x", tok, nil).StatusCode; c != 403 {
		t.Errorf("PUT: %d", c)
	}
	if c := do(t, "GET", srv.URL+"/p/up/other", tok, nil).StatusCode; c != 403 {
		t.Errorf("outside path: %d", c)
	}
	if c := do(t, "GET", srv.URL+"/p/up/v1/..%2f..%2fetc", tok, nil).StatusCode; c != 400 {
		t.Errorf("traversal: %d", c)
	}
	// COPY with a Destination outside the rules
	if c := do(t, "COPY", srv.URL+"/p/up/v1/a", tok, map[string]string{"Destination": srv.URL + "/p/up/secret/b"}).StatusCode; c != 403 {
		t.Errorf("copy dest: %d", c)
	}
	// cross-host redirect location is dropped
	if loc := do(t, "GET", srv.URL+"/p/up/v1/redir", tok, nil).Header.Get("Location"); loc != "" {
		t.Errorf("location leaked: %s", loc)
	}
	// echo connector shows what upstream would see: no Authorization
	e := do(t, "GET", srv.URL+"/p/echo/hello", tok, map[string]string{"Authorization": "Bearer " + tok})
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	n, _ := e.Body.Read(buf)
	b.Write(buf[:n])
	if e.StatusCode != 200 || strings.Contains(strings.ToLower(b.String()), "authorization") {
		t.Errorf("echo: %d %s", e.StatusCode, b)
	}
	// revoke-all -> 401 immediately
	g.RevokeAll()
	if c := do(t, "GET", srv.URL+"/p/up/v1/x", tok, nil).StatusCode; c != 401 {
		t.Errorf("after revoke: %d", c)
	}
}
