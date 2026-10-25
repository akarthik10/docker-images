// Package consent is the public-facing approval UI, gated by a fresh OIDC login (passkey via Pocket ID).
package consent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/akarthik10/keymaster/internal/grants"
)

//go:embed page.html
var pageHTML string

var tmpl = template.Must(template.New("page").Funcs(template.FuncMap{
	"dur": func(d time.Duration) string { return d.String() },
}).Parse(pageHTML))

const sessionTTL = 5 * time.Minute

type Config struct {
	PublicURL, Issuer, ClientID, ClientSecret, OwnerSub string
	AuthMaxAge                                          time.Duration
}

type Handler struct {
	G   *grants.Service
	Cfg Config

	mu       sync.Mutex
	provider *oidc.Provider
	states   map[string]loginState // oidc state -> pending login
	sessions map[string]*session   // session cookie -> authenticated browser
}

type loginState struct {
	grantID, verifier, nonce string // grantID "" => the /grants page
	exp                      time.Time
}

type session struct {
	grantID, csrf string
	exp           time.Time
}

func New(g *grants.Service, cfg Config) *Handler {
	return &Handler{G: g, Cfg: cfg, states: map[string]loginState{}, sessions: map[string]*session{}}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /approve/{id}", h.start)
	mux.HandleFunc("POST /approve/{id}", h.decide)
	mux.HandleFunc("GET /grants", h.listGrants)
	mux.HandleFunc("POST /grants/revoke", h.revoke)
	mux.HandleFunc("GET /oidc/callback", h.callback)
}

func rnd() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (h *Handler) oauth(ctx context.Context) (*oauth2.Config, *oidc.Provider, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.provider == nil {
		p, err := oidc.NewProvider(ctx, h.Cfg.Issuer)
		if err != nil {
			return nil, nil, err
		}
		h.provider = p
	}
	return &oauth2.Config{
		ClientID: h.Cfg.ClientID, ClientSecret: h.Cfg.ClientSecret, Endpoint: h.provider.Endpoint(),
		RedirectURL: h.Cfg.PublicURL + "/oidc/callback", Scopes: []string{oidc.ScopeOpenID},
	}, h.provider, nil
}

// login sends the browser to Pocket ID, forcing a fresh authentication every time.
func (h *Handler) login(w http.ResponseWriter, r *http.Request, grantID string) {
	oc, _, err := h.oauth(r.Context())
	if err != nil {
		http.Error(w, "identity provider unavailable", 502)
		return
	}
	st := loginState{grantID: grantID, verifier: oauth2.GenerateVerifier(), nonce: rnd(), exp: time.Now().Add(sessionTTL)}
	state := rnd()
	h.mu.Lock()
	h.states[state] = st
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "km_state", Value: state, Path: "/oidc/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, oc.AuthCodeURL(state, oauth2.S256ChallengeOption(st.verifier), oidc.Nonce(st.nonce),
		oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0")), http.StatusFound)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	g, err := h.G.Get(r.PathValue("id"))
	if err != nil || g.Status != "pending" {
		render(w, 404, "Request not found, or no longer pending.", nil)
		return
	}
	h.login(w, r, g.ID)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	c, err := r.Cookie("km_state")
	h.mu.Lock()
	st, ok := h.states[state]
	delete(h.states, state) // single use: replayed state fails
	h.mu.Unlock()
	if err != nil || !ok || !eq(c.Value, state) || time.Now().After(st.exp) {
		render(w, 400, "Login state invalid or expired.", nil)
		return
	}
	if err := h.verify(r, st); err != nil {
		h.G.St.Audit(time.Now().Unix(), st.grantID, "", "deny", "", "", "", 0, 0, "login rejected: "+err.Error())
		render(w, 403, "Login rejected: "+err.Error(), nil)
		return
	}
	sid := rnd()
	s := &session{grantID: st.grantID, csrf: rnd(), exp: time.Now().Add(sessionTTL)}
	h.mu.Lock()
	h.sessions[sid] = s
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "km_session", Value: sid, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 300})
	if st.grantID == "" {
		http.Redirect(w, r, "/grants", http.StatusSeeOther)
		return
	}
	h.showConsent(w, st.grantID, s)
}

func (h *Handler) verify(r *http.Request, st loginState) error {
	oc, prov, err := h.oauth(r.Context())
	if err != nil {
		return err
	}
	tok, err := oc.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(st.verifier))
	if err != nil {
		return errors.New("code exchange failed")
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := prov.Verifier(&oidc.Config{ClientID: h.Cfg.ClientID}).Verify(r.Context(), raw)
	if err != nil {
		return errors.New("bad id_token")
	}
	var cl struct {
		Nonce    string `json:"nonce"`
		AuthTime int64  `json:"auth_time"`
	}
	idt.Claims(&cl)
	switch {
	case !eq(cl.Nonce, st.nonce):
		return errors.New("nonce mismatch")
	case !eq(idt.Subject, h.Cfg.OwnerSub):
		return errors.New("not the owner")
	case cl.AuthTime != 0 && time.Since(time.Unix(cl.AuthTime, 0)) > h.Cfg.AuthMaxAge:
		return errors.New("authentication is not fresh")
	}
	// If the IdP omits auth_time we rely on prompt=login having forced re-authentication.
	return nil
}

func (h *Handler) sess(r *http.Request) *session {
	c, err := r.Cookie("km_session")
	if err != nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[c.Value]
	if s == nil || time.Now().After(s.exp) {
		delete(h.sessions, c.Value)
		return nil
	}
	return s
}

func (h *Handler) drop(r *http.Request) {
	if c, err := r.Cookie("km_session"); err == nil {
		h.mu.Lock()
		delete(h.sessions, c.Value)
		h.mu.Unlock()
	}
}

type scopeView struct {
	Name    string
	Write   bool
	Checked bool
}

type page struct {
	Message, CSRF, ID, Agent, Reason string
	Scopes                           []scopeView
	TTLs                             []int
	Active                           []*grants.Grant
	Warning                          bool
	Grants                           bool
	RememberDays                     int // >0 when a tier 1 scope is present

}

func builtinMax(a, b int) int { return max(a, b) }

func (h *Handler) showConsent(w http.ResponseWriter, id string, s *session) {
	g, err := h.G.Get(id)
	if err != nil || g.Status != "pending" {
		render(w, 404, "Request is no longer pending.", nil)
		return
	}
	pol := h.G.Pol
	active, _ := h.G.ListActive()
	p := &page{CSRF: s.csrf, ID: g.ID, Agent: g.Agent, Reason: g.Reason, Active: active}
	write, untrusted := false, false
	mark := func(sc string) {
		if _, c, sp, ok := pol.Scope(sc); ok {
			write = write || c.IsWrite(sp)
			untrusted = untrusted || (c.UntrustedContent && !c.IsWrite(sp))
		}
	}
	for _, sc := range g.Scopes {
		_, c, sp, _ := pol.Scope(sc)
		p.Scopes = append(p.Scopes, scopeView{sc, c.IsWrite(sp), true})
		mark(sc)
	}
	for _, a := range active {
		for _, sc := range a.Scopes {
			mark(sc)
		}
	}
	p.Warning = write && untrusted
	max := int(h.G.MaxTTL(g.Scopes).Minutes())
	for _, m := range []int{5, 15, 60, 240, 1440, 4320} {
		if m < max {
			p.TTLs = append(p.TTLs, m)
		}
	}
	p.TTLs = append(p.TTLs, max)
	for _, sc := range g.Scopes {
		if _, _, sp, ok := pol.Scope(sc); ok && sp.Tier == 1 {
			p.RememberDays = builtinMax(p.RememberDays, int((sp.MaxTTL+23*time.Hour)/(24*time.Hour)))
		}
	}
	render(w, 200, "", p)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	s := h.sess(r)
	id := r.PathValue("id")
	if s == nil || s.grantID != id || r.ParseForm() != nil || !eq(r.PostFormValue("csrf"), s.csrf) {
		render(w, 403, "Session expired. Open the approval link again to sign in.", nil)
		return
	}
	h.drop(r) // single use: one passkey login per decision
	if r.PostFormValue("action") != "approve" {
		h.G.Deny(id)
		render(w, 200, "Denied.", nil)
		return
	}
	mins, _ := strconv.Atoi(r.PostFormValue("ttl"))
	g, err := h.G.Approve(r.Context(), id, h.Cfg.OwnerSub, slices.Clone(r.PostForm["scope"]), time.Duration(mins)*time.Minute)
	if err != nil {
		render(w, 400, "Could not approve: "+err.Error(), nil)
		return
	}
	msg := fmt.Sprintf("Approved until %s. The agent will pick it up.", g.ExpiresAt.Format("15:04:05 MST"))
	if r.PostFormValue("remember") == "1" {
		if rem := h.G.Remember(g, h.Cfg.OwnerSub); len(rem) > 0 {
			msg += fmt.Sprintf(" Remembered: %v.", rem)
		}
	}
	render(w, 200, msg, nil)
}

func (h *Handler) listGrants(w http.ResponseWriter, r *http.Request) {
	s := h.sess(r)
	if s == nil || s.grantID != "" {
		h.login(w, r, "")
		return
	}
	active, _ := h.G.ListActive()
	render(w, 200, "", &page{Grants: true, CSRF: s.csrf, Active: active})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	s := h.sess(r)
	if s == nil || s.grantID != "" || r.ParseForm() != nil || !eq(r.PostFormValue("csrf"), s.csrf) {
		render(w, 403, "Session expired.", nil)
		return
	}
	if id := r.PostFormValue("id"); id == "all" {
		n, _ := h.G.RevokeAll()
		render(w, 200, fmt.Sprintf("Revoked %d grants.", n), nil)
	} else if err := h.G.Revoke(id); err != nil {
		render(w, 400, err.Error(), nil)
	} else {
		render(w, 200, "Revoked.", nil)
	}
}

func render(w http.ResponseWriter, code int, msg string, p *page) {
	if p == nil {
		p = &page{}
	}
	p.Message = msg
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(code)
	tmpl.Execute(w, p)
}
