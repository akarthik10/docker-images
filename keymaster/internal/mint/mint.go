// Package mint creates short-lived native tokens (k8s TokenRequest, GitHub App).
package mint

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akarthik10/keymaster/internal/kube"
)

// Request is what a minter gets: connector params, scope params, and the grant TTL.
type Request struct {
	Params, ScopeParams map[string]any
	TTL                 time.Duration
	CredsDir            string
}

// Result is handed to the agent once. Revoke (optional) is called when the grant ends.
type Result struct {
	Value  map[string]any
	Revoke func(context.Context) error
	// Handle (optional, JSON-serializable) lets a Restorer rebuild Revoke after a restart.
	Handle map[string]any
}

// Restorer is implemented by minters whose revocation survives a restart.
type Restorer interface {
	Restore(handle map[string]any) func(context.Context) error
}

type Minter interface {
	Mint(ctx context.Context, r Request) (*Result, error)
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func credFile(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) {
		return "", fmt.Errorf("bad credential file name %q", name)
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	return strings.TrimSpace(string(b)), err
}

// K8s mints a ServiceAccount token via TokenRequest.
type K8s struct{ Kube *kube.Client }

func (k K8s) Mint(ctx context.Context, r Request) (*Result, error) {
	secs := int64(r.TTL.Seconds())
	if secs < 600 {
		secs = 600 // API minimum
	}
	ns, sa := str(r.Params, "namespace"), str(r.Params, "serviceaccount")
	body := map[string]any{
		"apiVersion": "authentication.k8s.io/v1", "kind": "TokenRequest",
		"spec": map[string]any{"audiences": []string{str(r.Params, "audience")}, "expirationSeconds": secs},
	}
	var out struct {
		Status struct{ Token, ExpirationTimestamp string } `json:"status"`
	}
	if err := k.Kube.Do(ctx, "POST", fmt.Sprintf("/api/v1/namespaces/%s/serviceaccounts/%s/token", ns, sa), body, &out); err != nil {
		return nil, err
	}
	// k8s tokens can't be revoked; the short TTL is the control.
	return &Result{Value: map[string]any{"token": out.Status.Token, "expires_at": out.Status.ExpirationTimestamp}}, nil
}

// GitHubApp mints an installation token and deletes it on revoke.
type GitHubApp struct {
	API  string // default https://api.github.com
	HTTP *http.Client
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func appJWT(appID string, keyPEM []byte, now time.Time) (string, error) {
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return "", fmt.Errorf("github app key: no PEM block")
	}
	key, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		k8, err2 := x509.ParsePKCS8PrivateKey(blk.Bytes)
		rk, ok := k8.(*rsa.PrivateKey)
		if err2 != nil || !ok {
			return "", fmt.Errorf("github app key: %w", err)
		}
		key = rk
	}
	hdr, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT"})
	cl, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(5 * time.Minute).Unix(), "iss": appID})
	in := b64(hdr) + "." + b64(cl)
	h := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return in + "." + b64(sig), nil
}

func (g GitHubApp) call(ctx context.Context, method, path, bearer string, in, out any) error {
	api := g.API
	if api == "" {
		api = "https://api.github.com"
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, api+path, body)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
	c := g.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: %d %s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (g GitHubApp) Mint(ctx context.Context, r Request) (*Result, error) {
	appID, err := credFile(r.CredsDir, str(r.Params, "app_id_file"))
	if err != nil {
		return nil, err
	}
	inst, err := credFile(r.CredsDir, str(r.Params, "installation_id_file"))
	if err != nil {
		return nil, err
	}
	key, err := credFile(r.CredsDir, str(r.Params, "key_file"))
	if err != nil {
		return nil, err
	}
	jwt, err := appJWT(appID, []byte(key), time.Now())
	if err != nil {
		return nil, err
	}
	var tok struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	body := map[string]any{"repositories": r.ScopeParams["repositories"], "permissions": r.ScopeParams["permissions"]}
	if err := g.call(ctx, "POST", "/app/installations/"+inst+"/access_tokens", jwt, body, &tok); err != nil {
		return nil, err
	}
	h := map[string]any{"token": tok.Token}
	return &Result{Value: map[string]any{"token": tok.Token, "expires_at": tok.ExpiresAt}, Revoke: g.Restore(h), Handle: h}, nil
}

// Restore rebuilds the revoke function from a stored handle.
func (g GitHubApp) Restore(h map[string]any) func(context.Context) error {
	tok := str(h, "token")
	return func(ctx context.Context) error {
		return g.call(ctx, "DELETE", "/installation/token", tok, nil, nil)
	}
}
