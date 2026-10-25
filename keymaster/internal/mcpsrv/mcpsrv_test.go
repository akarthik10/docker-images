package mcpsrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/akarthik10/keymaster/internal/agentauth"
	"github.com/akarthik10/keymaster/internal/grants"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/store"
)

type tokenAuth struct{}

func (tokenAuth) Agent(r *http.Request) (string, error) {
	if r.Header.Get("Authorization") == "Bearer agent-a" {
		return "default/a", nil
	}
	return "", http.ErrNoCookie
}

var _ agentauth.Authenticator = tokenAuth{}

func TestToolsEndToEnd(t *testing.T) {
	p, _ := policy.Parse([]byte(`
agents: [{sa: default/a, allowed_connectors: [echo]}]
connectors:
  echo:
    kind: proxy
    upstream: "internal:echo"
    scopes:
      free: {tier: 0, max_ttl: 1h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
      any:  {tier: 2, max_ttl: 1h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
`))
	st, _ := store.Open(filepath.Join(t.TempDir(), "k.db"))
	defer st.Close()
	g := grants.New(st, p)
	srv := httptest.NewServer((&Server{G: g, Auth: tokenAuth{}, PublicURL: "https://km", ProxyURL: "http://km:8080", Wait: 100 * time.Millisecond}).Handler())
	defer srv.Close()

	// wrong token is rejected before MCP
	if r, _ := http.Get(srv.URL); r.StatusCode != 401 {
		t.Fatalf("unauthenticated: %d", r.StatusCode)
	}

	hc := &http.Client{Transport: roundTripper{"Bearer agent-a"}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: hc, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	call := func(name string, args map[string]any) map[string]any {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		return res.StructuredContent.(map[string]any)
	}

	if c := call("list_connectors", nil)["connectors"].([]any); len(c) != 1 {
		t.Fatalf("connectors: %v", c)
	}
	free := call("request_access", map[string]any{"scopes": []string{"echo:free"}, "reason": "x"})
	if free["status"] != "active" || free["grant_token"] == "" {
		t.Fatalf("tier 0: %v", free)
	}
	pend := call("request_access", map[string]any{"scopes": []string{"echo:any"}, "reason": "x"})
	if pend["status"] != "pending" || pend["approval_url"] == "" {
		t.Fatalf("pending: %v", pend)
	}
	if got := call("list_grants", nil)["grants"].([]any); len(got) != 2 {
		t.Fatalf("list_grants: %v", got)
	}
	if call("release", map[string]any{"grant_id": free["grant_id"]})["status"] != "released" {
		t.Fatal("release")
	}
}

type roundTripper struct{ auth string }

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", r.auth)
	return http.DefaultTransport.RoundTrip(req)
}
