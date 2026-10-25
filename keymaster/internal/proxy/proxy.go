// Package proxy is the credential-injecting reverse proxy at /p/<connector>/...
package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akarthik10/keymaster/internal/grants"
	"github.com/akarthik10/keymaster/internal/policy"
)

type Proxy struct {
	G        *grants.Service
	CredsDir string
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type statusWriter struct {
	http.ResponseWriter
	status, bytes int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (p *Proxy) cred(name string) string {
	if name == "" || name != filepath.Base(name) {
		return ""
	}
	b, _ := os.ReadFile(filepath.Join(p.CredsDir, name))
	return strings.TrimSpace(string(b))
}

// Handler serves /p/{connector}/{rest...}.
func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		name := r.PathValue("connector")
		var grantID, agent string
		defer func() {
			p.G.St.Audit(time.Now().Unix(), grantID, agent, "proxy", name, r.Method, r.URL.Path, sw.status, sw.bytes, "")
		}()
		p.serve(sw, r, name, &grantID, &agent)
	})
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request, name string, grantID, agent *string) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		jsonErr(w, 401, "missing grant token")
		return
	}
	g, err := p.G.Lookup(tok)
	if err != nil {
		jsonErr(w, 401, err.Error())
		return
	}
	*grantID, *agent = g.ID, g.Agent
	conn, ok := p.G.Pol.Connectors[name]
	if !ok || conn.Kind != "proxy" {
		jsonErr(w, 404, "unknown connector")
		return
	}
	// Scopes of this grant that belong to this connector.
	var scopes []policy.Scope
	var names []string
	for _, sc := range g.Scopes {
		if cn, _, s, ok := p.G.Pol.Scope(sc); ok && cn == name {
			scopes = append(scopes, s)
			names = append(names, sc)
		}
	}
	if len(scopes) == 0 {
		jsonErr(w, 403, "grant does not include connector "+name)
		return
	}
	prefix := "/p/" + name
	clean, ok := policy.CleanPath(strings.TrimPrefix(r.URL.EscapedPath(), prefix))
	if !ok {
		jsonErr(w, 400, "path rejected")
		return
	}
	// COPY/MOVE carry a second path in Destination; it must be allowed too.
	var dest string
	if d := r.Header.Get("Destination"); d != "" {
		du, err := url.Parse(d)
		dp, ok2 := "", false
		if err == nil {
			dp, ok2 = policy.CleanPath(strings.TrimPrefix(du.EscapedPath(), prefix))
		}
		if !ok2 || !allowed(scopes, r.Method, dp) {
			jsonErr(w, 403, "destination not allowed by granted scopes")
			return
		}
		dest = dp
	}
	if !allowed(scopes, r.Method, clean) {
		jsonErr(w, 403, "not allowed by granted scopes "+strings.Join(names, ",")+"; request a scope that permits "+r.Method+" "+clean)
		return
	}
	up, err := url.Parse(conn.Upstream)
	if err != nil {
		jsonErr(w, 500, "bad upstream")
		return
	}
	if conn.Upstream == "internal:echo" {
		echo(w, r, clean)
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = up.Scheme, up.Host
			pr.Out.URL.Path, pr.Out.URL.RawPath = clean, ""
			pr.Out.Host = up.Host
			p.inject(pr.Out.Header, conn)
			if dest != "" {
				pr.Out.Header.Set("Destination", up.Scheme+"://"+up.Host+dest)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if loc := resp.Header.Get("Location"); loc != "" {
				lu, err := url.Parse(loc)
				switch {
				case err == nil && !lu.IsAbs():
					// relative redirect stays on the upstream path space
					lu.Path = prefix + lu.Path
					resp.Header.Set("Location", lu.String())
				case err == nil && lu.Host == up.Host:
					resp.Header.Set("Location", prefix+lu.EscapedPath())
				default:
					resp.Header.Del("Location")
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) { jsonErr(w, 502, "upstream error") },
	}
	rp.ServeHTTP(w, r)
}

func allowed(scopes []policy.Scope, method, path string) bool {
	for _, s := range scopes {
		if s.Allows(method, path) {
			return true
		}
	}
	return false
}

// inject strips everything the agent sent that could carry auth, then adds the connector's.
func (p *Proxy) inject(h http.Header, c policy.Connector) {
	for k := range h {
		if lk := strings.ToLower(k); lk == "authorization" || lk == "cookie" || strings.HasPrefix(lk, "proxy-") {
			h.Del(k)
		}
	}
	switch c.Auth["type"] {
	case "basic":
		h.Set("Authorization", "Basic "+basic(p.cred(c.Auth["user_file"]), p.cred(c.Auth["pass_file"])))
	case "bearer":
		h.Set("Authorization", "Bearer "+p.cred(c.Auth["value_file"]))
	case "header":
		if n := c.Auth["name"]; n != "" {
			for k := range h {
				if strings.EqualFold(k, n) {
					h.Del(k) // don't let the agent set it
				}
			}
			h.Set(n, p.cred(c.Auth["value_file"]))
		}
	}
}

// echo is the built-in test connector: it reflects what the "upstream" would receive.
func echo(w http.ResponseWriter, r *http.Request, path string) {
	h := r.Header.Clone()
	for k := range h {
		if lk := strings.ToLower(k); lk == "authorization" || lk == "cookie" || strings.HasPrefix(lk, "proxy-") {
			h.Del(k)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "path": path, "headers": h})
}
