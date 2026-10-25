package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/akarthik10/keymaster/internal/agentauth"
	"github.com/akarthik10/keymaster/internal/consent"
	"github.com/akarthik10/keymaster/internal/grants"
	"github.com/akarthik10/keymaster/internal/kube"
	"github.com/akarthik10/keymaster/internal/mcpsrv"
	"github.com/akarthik10/keymaster/internal/mint"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/proxy"
	"github.com/akarthik10/keymaster/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	pol, err := policy.Load(env("KM_POLICY_PATH", "/etc/keymaster/policy.yaml"))
	if err != nil {
		return err // bad policy => crash with a clear error
	}
	st, err := store.Open(env("KM_DB_PATH", "/data/keymaster.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	st.Audit(time.Now().Unix(), "", "", "policy_load", "", "", "", 0, 0, "connectors="+strconv.Itoa(len(pol.Connectors)))

	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			err = errors.Join(err, errors.New("missing env "+k))
		}
		return v
	}
	publicURL, issuer, clientID := need("KM_PUBLIC_URL"), need("KM_OIDC_ISSUER"), need("KM_OIDC_CLIENT_ID")
	clientSecret, ownerSub := need("KM_OIDC_CLIENT_SECRET"), need("KM_OWNER_SUB")
	if err != nil {
		return err
	}
	credsDir := env("KM_CREDS_DIR", "/etc/keymaster/creds")
	wait, _ := time.ParseDuration(env("KM_WAIT", "90s"))

	g := grants.New(st, pol)
	g.PublicURL, g.CredsDir = publicURL, credsDir

	var auth agentauth.Authenticator
	if dev := os.Getenv("KM_DEV_AGENT"); dev != "" {
		log.Warn("KM_DEV_AGENT set: agent authentication is DISABLED", "agent", dev)
		auth = agentauth.Static(dev)
	} else {
		kc, err := kube.InCluster()
		if err != nil {
			return err
		}
		auth = agentauth.TokenReview{Kube: kc}
		g.Minters["k8s_tokenrequest"] = mint.K8s{Kube: kc}
	}
	g.Minters["github_app"] = mint.GitHubApp{}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	g.Recover()
	go g.RunReaper(ctx)

	health := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }

	// :8080 internal (agents only, no ingress)
	internal := http.NewServeMux()
	internal.HandleFunc("GET /healthz", health)
	internal.Handle("/mcp", (&mcpsrv.Server{G: g, Auth: auth, PublicURL: publicURL, Wait: wait,
		ProxyURL: env("KM_PROXY_URL", "http://keymaster.default.svc.cluster.local:8080")}).Handler())
	internal.Handle("/p/{connector}/", (&proxy.Proxy{G: g, CredsDir: credsDir}).Handler())

	// :8081 public (via Traefik): the OIDC login is the auth
	public := http.NewServeMux()
	public.HandleFunc("GET /healthz", health)
	consent.New(g, consent.Config{PublicURL: publicURL, Issuer: issuer, ClientID: clientID,
		ClientSecret: clientSecret, OwnerSub: ownerSub, AuthMaxAge: pol.Defaults.AuthMaxAge}).Register(public)

	servers := []*http.Server{
		{Addr: env("KM_INTERNAL_ADDR", ":8080"), Handler: internal, ReadHeaderTimeout: 10 * time.Second},
		{Addr: env("KM_PUBLIC_ADDR", ":8081"), Handler: public, ReadHeaderTimeout: 10 * time.Second},
	}
	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			log.Info("listening", "addr", s.Addr)
			errc <- s.ListenAndServe()
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(ctx)
	}
	return nil
}
