// Package agentauth identifies the calling agent from its projected SA token.
package agentauth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/akarthik10/keymaster/internal/kube"
)

// Authenticator maps a request to an agent identity ("namespace/name").
type Authenticator interface {
	Agent(r *http.Request) (string, error)
}

type TokenReview struct{ Kube *kube.Client }

func (t TokenReview) Agent(r *http.Request) (string, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return "", errors.New("missing bearer token")
	}
	req := map[string]any{
		"apiVersion": "authentication.k8s.io/v1", "kind": "TokenReview",
		"spec": map[string]any{"token": tok, "audiences": []string{"keymaster"}},
	}
	var res struct {
		Status struct {
			Authenticated bool `json:"authenticated"`
			User          struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"status"`
	}
	if err := t.Kube.Do(context.Background(), "POST", "/apis/authentication.k8s.io/v1/tokenreviews", req, &res); err != nil {
		return "", err
	}
	ns, name, ok := parseSA(res.Status.User.Username)
	if !res.Status.Authenticated || !ok {
		return "", errors.New("token not authenticated as a service account")
	}
	return ns + "/" + name, nil
}

func parseSA(u string) (ns, name string, ok bool) {
	rest, ok := strings.CutPrefix(u, "system:serviceaccount:")
	if !ok {
		return
	}
	ns, name, ok = strings.Cut(rest, ":")
	return ns, name, ok && ns != "" && name != ""
}

// Static is for local development only (KM_DEV_AGENT).
type Static string

func (s Static) Agent(*http.Request) (string, error) { return string(s), nil }
