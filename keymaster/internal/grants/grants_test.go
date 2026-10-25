package grants

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/akarthik10/keymaster/internal/mint"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/store"
)

const pol = `
agents:
  - sa: default/a
    allowed_connectors: [echo, k8s]
connectors:
  echo:
    kind: proxy
    upstream: "internal:echo"
    scopes:
      free: {tier: 0, max_ttl: 1h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
      any:  {tier: 2, max_ttl: 1h, rules: [{methods: [GET, PUT], paths: ["^/.*$"]}]}
      no:   {tier: 3}
      note: {tier: 1, max_ttl: 72h, rules: [{methods: [GET], paths: ["^/.*$"]}]}
  k8s:
    kind: mint
    minter: fake
    scopes:
      write: {tier: 2, max_ttl: 10m}
`

type fakeMinter struct{ revoked *bool }

func (f fakeMinter) Mint(context.Context, mint.Request) (*mint.Result, error) {
	return &mint.Result{Value: map[string]any{"token": "t"}, Revoke: f.Restore(nil), Handle: map[string]any{"token": "t"}}, nil
}

func (f fakeMinter) Restore(map[string]any) func(context.Context) error {
	return func(context.Context) error { *f.revoked = true; return nil }
}

func setup(t *testing.T) (*Service, *time.Time, *bool) {
	p, err := policy.Parse([]byte(pol))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	revoked := false
	s := New(st, p)
	s.Now = func() time.Time { return now }
	s.Minters["fake"] = fakeMinter{&revoked}
	return s, &now, &revoked
}

func TestLifecycle(t *testing.T) {
	s, now, _ := setup(t)
	ctx := context.Background()

	// tier 0 auto-approves; token works, then expires.
	g, err := s.Request(ctx, "default/a", []string{"echo:free"}, "r", 0)
	if err != nil || g.Status != "active" {
		t.Fatalf("tier0: %v %+v", err, g)
	}
	_, d, _ := s.Wait(ctx, "default/a", g.ID, 0)
	if d == nil || d.Token == "" {
		t.Fatal("expected delivery")
	}
	if _, d2, _ := s.Wait(ctx, "default/a", g.ID, 0); d2 != nil {
		t.Fatal("delivery must be one-shot")
	}
	if _, err := s.Lookup(d.Token); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(16 * time.Minute)
	if _, err := s.Lookup(d.Token); err == nil {
		t.Fatal("token should be expired")
	}
	s.Reap()
	if g, _ := s.Get(g.ID); g.Status != "expired" {
		t.Fatalf("want expired, got %s", g.Status)
	}
}

func TestRequestRules(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	if _, err := s.Request(ctx, "default/a", []string{"echo:no"}, "", 0); err == nil {
		t.Error("tier 3 must be refused")
	}
	if _, err := s.Request(ctx, "default/b", []string{"echo:any"}, "", 0); err == nil {
		t.Error("unlisted agent must be refused")
	}
	if _, err := s.Request(ctx, "default/a", []string{"echo:nope"}, "", 0); err == nil {
		t.Error("unknown scope must be refused")
	}
	g, _ := s.Request(ctx, "default/a", []string{"echo:any"}, "", 5*time.Hour)
	if g.RequestedTTL != time.Hour {
		t.Errorf("ttl not capped: %v", g.RequestedTTL)
	}
}

func TestApproveNarrowDoubleAndRevoke(t *testing.T) {
	s, _, revoked := setup(t)
	ctx := context.Background()
	g, _ := s.Request(ctx, "default/a", []string{"echo:any", "k8s:write"}, "", 10*time.Minute)
	if g.Status != "pending" {
		t.Fatal("should be pending")
	}
	if _, err := s.Approve(ctx, g.ID, "ani", []string{"echo:free"}, time.Minute); err == nil {
		t.Error("approving an unrequested scope must fail")
	}
	if _, err := s.Approve(ctx, g.ID, "ani", []string{"echo:any"}, 2*time.Hour); err == nil {
		t.Error("ttl above cap must fail")
	}
	a, err := s.Approve(ctx, g.ID, "ani", []string{"k8s:write"}, 10*time.Minute)
	if err != nil || len(a.Scopes) != 1 {
		t.Fatalf("approve: %v %+v", err, a)
	}
	if _, err := s.Approve(ctx, g.ID, "ani", []string{"k8s:write"}, time.Minute); err == nil {
		t.Error("double approve must fail")
	}
	if n, _ := s.RevokeAll(); n != 1 || !*revoked {
		t.Errorf("revoke-all n=%d revoked=%v", n, *revoked)
	}
	if err := s.Release("default/a", g.ID); err == nil {
		t.Error("release after revoke should fail")
	}
}

func TestPendingExpires(t *testing.T) {
	s, now, _ := setup(t)
	g, _ := s.Request(context.Background(), "default/a", []string{"echo:any"}, "", 0)
	*now = now.Add(11 * time.Minute)
	if _, err := s.Approve(context.Background(), g.ID, "ani", []string{"echo:any"}, time.Minute); err == nil {
		t.Error("expired request must not be approvable")
	}
	s.Reap()
	if g, _ := s.Get(g.ID); g.Status != "expired" {
		t.Errorf("want expired, got %s", g.Status)
	}
}

func TestRememberedTier1(t *testing.T) {
	s, now, _ := setup(t)
	ctx := context.Background()
	g, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", time.Hour)
	if g.Status != "pending" {
		t.Fatal("tier 1 needs approval the first time")
	}
	a, err := s.Approve(ctx, g.ID, "ani", []string{"echo:note"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Remember(a, "ani"); len(got) != 1 {
		t.Fatalf("remembered %v", got)
	}
	// second request auto-approves, no passkey
	g2, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", 0)
	if g2.Status != "active" || g2.ApproverSub != "auto:remembered" {
		t.Fatalf("want auto-approved, got %+v", g2)
	}
	// mixed with a tier 2 scope still needs a human
	if g3, _ := s.Request(ctx, "default/a", []string{"echo:note", "echo:any"}, "", 0); g3.Status != "pending" {
		t.Error("tier 2 scope must still be pending")
	}
	// tier 2 scopes are never remembered
	g4, _ := s.Request(ctx, "default/a", []string{"echo:any"}, "", 0)
	a4, _ := s.Approve(ctx, g4.ID, "ani", []string{"echo:any"}, time.Minute)
	if len(s.Remember(a4, "ani")) != 0 {
		t.Error("tier 2 must not be remembered")
	}
	// revoking forgets
	if err := s.Revoke(g2.ID); err != nil {
		t.Fatal(err)
	}
	if g5, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", 0); g5.Status != "pending" {
		t.Error("revoke must forget remembered consent")
	}
	// expiry
	s.Remember(a, "ani")
	*now = now.Add(73 * time.Hour)
	if g6, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", 0); g6.Status != "pending" {
		t.Error("remembered consent must expire")
	}
}

func TestRevokeAllForgets(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	g, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", 0)
	a, _ := s.Approve(ctx, g.ID, "ani", []string{"echo:note"}, time.Hour)
	s.Remember(a, "ani")
	s.RevokeAll()
	if g2, _ := s.Request(ctx, "default/a", []string{"echo:note"}, "", 0); g2.Status != "pending" {
		t.Error("revoke-all must forget")
	}
}

func TestRecoverRearmsRevocation(t *testing.T) {
	s, now, revoked := setup(t)
	ctx := context.Background()
	g, _ := s.Request(ctx, "default/a", []string{"k8s:write"}, "", 10*time.Minute)
	if _, err := s.Approve(ctx, g.ID, "ani", []string{"k8s:write"}, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	// "restart": a fresh service on the same database
	s2 := New(s.St, s.Pol)
	s2.Now = s.Now
	s2.Minters["fake"] = fakeMinter{revoked}
	s2.Recover()
	if *revoked {
		t.Fatal("active grant must not be revoked on recovery")
	}
	*now = now.Add(11 * time.Minute)
	s2.Reap()
	if !*revoked {
		t.Fatal("upstream revoke must run after restart")
	}
	var hj *string
	s.St.DB.QueryRow(`SELECT mint_handles_json FROM grants WHERE id=?`, g.ID).Scan(&hj)
	if hj != nil {
		t.Error("handles must be cleared once revoked")
	}
}

func TestRecoverRevokesAlreadyEnded(t *testing.T) {
	s, now, revoked := setup(t)
	ctx := context.Background()
	g, _ := s.Request(ctx, "default/a", []string{"k8s:write"}, "", 10*time.Minute)
	s.Approve(ctx, g.ID, "ani", []string{"k8s:write"}, 10*time.Minute)
	*now = now.Add(11 * time.Minute) // expired while keymaster was down
	s2 := New(s.St, s.Pol)
	s2.Now = s.Now
	s2.Minters["fake"] = fakeMinter{revoked}
	s2.Recover()
	if !*revoked {
		t.Fatal("expired grant's token must be revoked at startup")
	}
}
