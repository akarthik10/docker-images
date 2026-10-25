package mint

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubMintAndRevoke(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/app/installations/42/access_tokens":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
				t.Error("expected app JWT")
			}
			w.Write([]byte(`{"token":"ghs_x","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == "DELETE" && r.URL.Path == "/installation/token":
			deleted = r.Header.Get("Authorization") == "Bearer ghs_x"
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	os.WriteFile(filepath.Join(dir, "id"), []byte("1\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "inst"), []byte("42\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "key"), pemKey, 0o600)

	res, err := GitHubApp{API: srv.URL}.Mint(context.Background(), Request{
		Params:      map[string]any{"app_id_file": "id", "installation_id_file": "inst", "key_file": "key"},
		ScopeParams: map[string]any{"repositories": []string{"r"}},
		TTL:         time.Hour, CredsDir: dir,
	})
	if err != nil || res.Value["token"] != "ghs_x" {
		t.Fatalf("mint: %v %v", res, err)
	}
	if err := res.Revoke(context.Background()); err != nil || !deleted {
		t.Fatalf("revoke: %v deleted=%v", err, deleted)
	}
}

func TestCredFileRejectsTraversal(t *testing.T) {
	if _, err := credFile(t.TempDir(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}
