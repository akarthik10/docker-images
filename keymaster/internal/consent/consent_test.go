package consent

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/akarthik10/keymaster/internal/grants"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/store"
)

// fakeIdP is a minimal OIDC provider whose next ID token is fully controlled by the test.
type fakeIdP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	sub    string
	age    time.Duration // how long ago the user authenticated
	nonce  string
	gotURL *url.URL
}

func newIdP(t *testing.T) *fakeIdP {
	f := &fakeIdP{sub: "owner", nonce: ""}
	f.key, _ = rsa.GenerateKey(rand.Reader, 2048)
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		sg, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: "k"}}, (&jose.SignerOptions{}).WithType("JWT"))
		now := time.Now()
		raw, _ := jwt.Signed(sg).Claims(map[string]any{
			"iss": f.srv.URL, "sub": f.sub, "aud": "km", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
			"nonce": f.nonce, "auth_time": now.Add(-f.age).Unix(),
		}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": raw})
	})
	return f
}

type env struct {
	h   *Handler
	g   *grants.Service
	idp *fakeIdP
	web *httptest.Server
}

func setup(t *testing.T) *env {
	idp := newIdP(t)
	p, err := policy.Parse([]byte(`
agents: [{sa: default/a, allowed_connectors: [echo]}]
connectors:
  echo:
    kind: proxy
    upstream: "internal:echo"
    scopes:
      any: {tier: 2, max_ttl: 1h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	st, _ := store.Open(filepath.Join(t.TempDir(), "k.db"))
	t.Cleanup(func() { st.Close() })
	g := grants.New(st, p)
	h := New(g, Config{PublicURL: "http://km.test", Issuer: idp.srv.URL, ClientID: "km", ClientSecret: "s", OwnerSub: "owner", AuthMaxAge: 2 * time.Minute})
	mux := http.NewServeMux()
	h.Register(mux)
	web := httptest.NewServer(mux)
	t.Cleanup(web.Close)
	return &env{h, g, idp, web}
}

var noFollow = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (e *env) do(t *testing.T, method, path string, form url.Values, cookies ...*http.Cookie) (*http.Response, string) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, e.web.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func cookie(r *http.Response, name string) *http.Cookie {
	for _, c := range r.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login runs /approve/<id> up to the callback and returns the callback response.
func (e *env) login(t *testing.T, id string) (cb *http.Response, body string, state *http.Cookie, stateVal string) {
	r, _ := e.do(t, "GET", "/approve/"+id, nil)
	loc, _ := url.Parse(r.Header.Get("Location"))
	q := loc.Query()
	if q.Get("prompt") != "login" || q.Get("max_age") != "0" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("login must force fresh auth with PKCE: %v", q)
	}
	e.idp.nonce = q.Get("nonce")
	state, stateVal = cookie(r, "km_state"), q.Get("state")
	cb, body = e.do(t, "GET", "/oidc/callback?code=c&state="+stateVal, nil, state)
	return
}

func pendingGrant(t *testing.T, e *env) string {
	g, err := e.g.Request(context.Background(), "default/a", []string{"echo:any"}, "<script>alert(1)</script>", 0)
	if err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func TestApproveFlow(t *testing.T) {
	e := setup(t)
	id := pendingGrant(t, e)
	cb, body, _, _ := e.login(t, id)
	if cb.StatusCode != 200 || !strings.Contains(body, "&lt;script&gt;") || strings.Contains(body, "<script>alert") {
		t.Fatalf("consent page: %d (reason must be escaped)\n%s", cb.StatusCode, body)
	}
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)[1]
	sess := cookie(cb, "km_session")
	form := url.Values{"csrf": {csrf}, "action": {"approve"}, "scope": {"echo:any"}, "ttl": {"15"}}

	if r, _ := e.do(t, "POST", "/approve/"+id, url.Values{"csrf": {"wrong"}, "action": {"approve"}}, sess); r.StatusCode != 403 {
		t.Errorf("bad csrf: %d", r.StatusCode)
	}
	// the bad-csrf attempt must not have burned the session, so the real one works:
	if r, _ := e.do(t, "POST", "/approve/"+id, form, sess); r.StatusCode != 200 {
		t.Fatalf("approve: %d", r.StatusCode)
	}
	if g, _ := e.g.Get(id); g.Status != "active" || g.GrantedTTL != 15*time.Minute {
		t.Fatalf("grant: %+v", g)
	}
	// replaying the same session/form (double approve) fails
	if r, _ := e.do(t, "POST", "/approve/"+id, form, sess); r.StatusCode != 403 {
		t.Errorf("double approve: %d", r.StatusCode)
	}
	// a second approval needs a fresh login: the old session is not valid for it
	id2 := pendingGrant(t, e)
	if r, _ := e.do(t, "POST", "/approve/"+id2, form, sess); r.StatusCode != 403 {
		t.Errorf("old session reused for second grant: %d", r.StatusCode)
	}
}

func TestLoginRejections(t *testing.T) {
	e := setup(t)
	id := pendingGrant(t, e)

	e.idp.age = 10 * time.Minute
	if cb, _, _, _ := e.login(t, id); cb.StatusCode != 403 {
		t.Errorf("stale auth_time: %d", cb.StatusCode)
	}
	e.idp.age, e.idp.sub = 0, "someone-else"
	if cb, _, _, _ := e.login(t, id); cb.StatusCode != 403 {
		t.Errorf("wrong sub: %d", cb.StatusCode)
	}
	e.idp.sub = "owner"
	cb, _, st, stateVal := e.login(t, id)
	if cb.StatusCode != 200 {
		t.Fatalf("good login: %d", cb.StatusCode)
	}
	if r, _ := e.do(t, "GET", "/oidc/callback?code=c&state="+stateVal, nil, st); r.StatusCode != 400 {
		t.Errorf("replayed state: %d", r.StatusCode)
	}
}

func TestGrantsPageNeedsLogin(t *testing.T) {
	e := setup(t)
	r, _ := e.do(t, "GET", "/grants", nil)
	if r.StatusCode != 302 || !strings.HasPrefix(r.Header.Get("Location"), e.idp.srv.URL) {
		t.Fatalf("expected redirect to IdP, got %d %s", r.StatusCode, r.Header.Get("Location"))
	}
}
