package kube

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDo(t *testing.T) {
	tokPath := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokPath, []byte("one\n"), 0o600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			http.Error(w, "forbidden", 403)
			return
		}
		b, _ := io.ReadAll(r.Body)
		w.Write([]byte(`{"auth":"` + r.Header.Get("Authorization") + `","body":` + string(b) + `}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client(), TokenPath: tokPath}

	var out struct {
		Auth string
		Body map[string]string
	}
	if err := c.Do(t.Context(), "POST", "/x", map[string]string{"a": "b"}, &out); err != nil || out.Auth != "Bearer one" || out.Body["a"] != "b" {
		t.Fatalf("%+v %v", out, err)
	}
	// the token file rotates; it must be re-read on every call
	os.WriteFile(tokPath, []byte("two"), 0o600)
	c.Do(t.Context(), "POST", "/x", map[string]string{}, &out)
	if out.Auth != "Bearer two" {
		t.Errorf("token not re-read: %q", out.Auth)
	}
	if err := c.Do(t.Context(), "GET", "/fail", nil, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("want 403 error, got %v", err)
	}
}
