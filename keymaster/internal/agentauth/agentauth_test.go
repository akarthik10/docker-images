package agentauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/akarthik10/keymaster/internal/kube"
)

func fakeKube(t *testing.T, user string, authed bool) *kube.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Spec struct {
				Token     string
				Audiences []string
			}
		}
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/apis/authentication.k8s.io/v1/tokenreviews" || len(req.Spec.Audiences) != 1 || req.Spec.Audiences[0] != "keymaster" {
			t.Errorf("bad review request: %s %+v", r.URL.Path, req)
		}
		json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"authenticated": authed, "user": map[string]any{"username": user}}})
	}))
	t.Cleanup(srv.Close)
	tok := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tok, []byte("km-own"), 0o600)
	return &kube.Client{Base: srv.URL, HTTP: srv.Client(), TokenPath: tok}
}

func req(auth string) *http.Request {
	r := httptest.NewRequest("POST", "/mcp", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	return r
}

func TestTokenReview(t *testing.T) {
	ok := TokenReview{fakeKube(t, "system:serviceaccount:default:claude-agent", true)}
	if a, err := ok.Agent(req("Bearer x")); err != nil || a != "default/claude-agent" {
		t.Fatalf("got %q %v", a, err)
	}
	for name, tr := range map[string]TokenReview{
		"unauthenticated": {fakeKube(t, "system:serviceaccount:default:x", false)},
		"not an SA":       {fakeKube(t, "admin", true)},
		"malformed SA":    {fakeKube(t, "system:serviceaccount:default", true)},
	} {
		if a, err := tr.Agent(req("Bearer x")); err == nil {
			t.Errorf("%s: accepted as %q", name, a)
		}
	}
	for _, h := range []string{"", "Bearer ", "Basic abc"} {
		if _, err := ok.Agent(req(h)); err == nil {
			t.Errorf("header %q must be rejected", h)
		}
	}
}

func TestParseSA(t *testing.T) {
	if ns, n, ok := parseSA("system:serviceaccount:a:b"); !ok || ns != "a" || n != "b" {
		t.Error("valid SA")
	}
	for _, u := range []string{"", "a:b", "system:serviceaccount:", "system:serviceaccount:a:", "system:serviceaccount::b"} {
		if _, _, ok := parseSA(u); ok {
			t.Errorf("%q must not parse", u)
		}
	}
}
