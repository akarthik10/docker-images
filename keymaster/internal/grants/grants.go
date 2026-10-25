// Package grants holds the grant state machine: request, approve, deny, release, expire.
package grants

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/akarthik10/keymaster/internal/mint"
	"github.com/akarthik10/keymaster/internal/policy"
	"github.com/akarthik10/keymaster/internal/store"
)

type Grant struct {
	ID, Agent, Status, Reason, ApproverSub string
	Scopes                                 []string
	RequestedTTL, GrantedTTL               time.Duration
	CreatedAt, ApprovedAt, ExpiresAt       time.Time
}

// Delivery is the secret part of an approved grant, handed to the agent exactly once.
type Delivery struct {
	Token  string
	Minted map[string]any // "connector:scope" -> minted value
}

// storedHandle is the persisted form of an upstream revocation handle.
type storedHandle struct {
	Minter string         `json:"minter"`
	Handle map[string]any `json:"handle"`
}

type Notifier interface {
	Notify(ctx context.Context, g *Grant, approvalURL string) error
}

type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, *Grant, string) error { return nil }

type Service struct {
	St        *store.Store
	Pol       *policy.Policy
	Minters   map[string]mint.Minter
	Notify    Notifier
	PublicURL string
	CredsDir  string
	Now       func() time.Time

	mu       sync.Mutex
	delivery map[string]*Delivery
	revokers map[string][]func(context.Context) error
}

func New(st *store.Store, pol *policy.Policy) *Service {
	return &Service{St: st, Pol: pol, Minters: map[string]mint.Minter{}, Notify: NopNotifier{}, Now: time.Now,
		delivery: map[string]*Delivery{}, revokers: map[string][]func(context.Context) error{}}
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func rnd() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) audit(g *Grant, event, detail string) {
	id, agent := "", ""
	if g != nil {
		id, agent = g.ID, g.Agent
	}
	s.St.Audit(s.Now().Unix(), id, agent, event, "", "", "", 0, 0, detail)
}

const cols = `id,agent,status,scopes_json,requested_ttl,COALESCE(granted_ttl,0),COALESCE(reason,''),created_at,COALESCE(approved_at,0),COALESCE(expires_at,0),COALESCE(approver_sub,'')`

type scanner interface{ Scan(...any) error }

func scan(r scanner) (*Grant, error) {
	var g Grant
	var sc string
	var rt, gt, c, a, e int64
	if err := r.Scan(&g.ID, &g.Agent, &g.Status, &sc, &rt, &gt, &g.Reason, &c, &a, &e, &g.ApproverSub); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(sc), &g.Scopes)
	g.RequestedTTL, g.GrantedTTL = time.Duration(rt)*time.Second, time.Duration(gt)*time.Second
	g.CreatedAt, g.ApprovedAt, g.ExpiresAt = time.Unix(c, 0), time.Unix(a, 0), time.Unix(e, 0)
	return &g, nil
}

func (s *Service) Get(id string) (*Grant, error) {
	return scan(s.St.DB.QueryRow(`SELECT `+cols+` FROM grants WHERE id=?`, id))
}

func (s *Service) list(where string, args ...any) ([]*Grant, error) {
	rows, err := s.St.DB.Query(`SELECT `+cols+` FROM grants WHERE `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Grant
	for rows.Next() {
		g, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListAgent returns the agent's pending and active grants.
func (s *Service) ListAgent(agent string) ([]*Grant, error) {
	return s.list(`agent=? AND (status='pending' OR (status='active' AND expires_at>?))`, agent, s.Now().Unix())
}

// ListActive returns all active, unexpired grants.
func (s *Service) ListActive() ([]*Grant, error) {
	return s.list(`status='active' AND expires_at>?`, s.Now().Unix())
}

// MaxTTL is the longest TTL allowed for this set of scopes.
func (s *Service) MaxTTL(scopes []string) time.Duration {
	var m time.Duration
	for _, sc := range scopes {
		if _, _, sp, ok := s.Pol.Scope(sc); ok && (m == 0 || sp.MaxTTL < m) {
			m = sp.MaxTTL
		}
	}
	return m
}

// Request creates a pending grant (or an active one if every scope is tier 0).
func (s *Service) Request(ctx context.Context, agent string, scopes []string, reason string, ttl time.Duration) (*Grant, error) {
	if len(scopes) == 0 {
		return nil, errors.New("no scopes requested")
	}
	preApproved, usedRemembered := true, false
	remaining := time.Duration(0)
	for _, sc := range scopes {
		conn, _, sp, ok := s.Pol.Scope(sc)
		switch {
		case !ok:
			return nil, fmt.Errorf("unknown scope %q", sc)
		case !s.Pol.AgentAllows(agent, conn):
			return nil, fmt.Errorf("agent %s may not use connector %q", agent, conn)
		case sp.Tier == 3:
			return nil, fmt.Errorf("scope %q is never grantable", sc)
		case sp.Tier == 1:
			if left, ok := s.remembered(agent, sc); ok {
				usedRemembered = true
				if remaining == 0 || left < remaining {
					remaining = left
				}
			} else {
				preApproved = false
			}
		case sp.Tier > 0:
			preApproved = false
		}
	}
	if ttl <= 0 {
		ttl = s.Pol.Defaults.ProxyTTL
	}
	ttl = min(ttl, s.MaxTTL(scopes))
	if usedRemembered && preApproved {
		ttl = min(ttl, remaining)
	}
	sj, _ := json.Marshal(scopes)
	g := &Grant{ID: rnd()[:22], Agent: agent, Status: "pending", Scopes: scopes, Reason: reason, RequestedTTL: ttl, CreatedAt: s.Now()}
	_, err := s.St.DB.Exec(`INSERT INTO grants(id,agent,status,scopes_json,requested_ttl,reason,created_at) VALUES(?,?,?,?,?,?,?)`,
		g.ID, agent, "pending", string(sj), int64(ttl.Seconds()), reason, g.CreatedAt.Unix())
	if err != nil {
		return nil, err
	}
	s.audit(g, "request", strings.Join(scopes, ","))
	if preApproved {
		sub := "auto:tier0"
		if usedRemembered {
			sub = "auto:remembered"
		}
		return s.Approve(ctx, g.ID, sub, scopes, ttl)
	}
	s.Notify.Notify(ctx, g, s.PublicURL+"/approve/"+g.ID)
	return g, nil
}

// remembered reports whether the owner has a live remembered consent for agent+scope.
func (s *Service) remembered(agent, scope string) (time.Duration, bool) {
	var exp int64
	err := s.St.DB.QueryRow(`SELECT expires_at FROM remembered WHERE agent=? AND scope=? AND expires_at>?`,
		agent, scope, s.Now().Unix()).Scan(&exp)
	if err != nil {
		return 0, false
	}
	return time.Unix(exp, 0).Sub(s.Now()), true
}

// Remember stores the owner's consent for the tier 1 scopes of an approved grant. The consent
// lasts each scope's max_ttl; tier 0 and 2 scopes are ignored. Returns the scopes remembered.
func (s *Service) Remember(g *Grant, sub string) []string {
	var out []string
	for _, sc := range g.Scopes {
		_, _, sp, ok := s.Pol.Scope(sc)
		if !ok || sp.Tier != 1 {
			continue
		}
		now := s.Now()
		if _, err := s.St.DB.Exec(`INSERT OR REPLACE INTO remembered(agent,scope,approver_sub,created_at,expires_at) VALUES(?,?,?,?,?)`,
			g.Agent, sc, sub, now.Unix(), now.Add(sp.MaxTTL).Unix()); err == nil {
			out = append(out, sc)
		}
	}
	if len(out) > 0 {
		s.audit(g, "remember", strings.Join(out, ","))
	}
	return out
}

func (s *Service) forget(agent string, scopes []string) {
	for _, sc := range scopes {
		s.St.DB.Exec(`DELETE FROM remembered WHERE agent=? AND scope=?`, agent, sc)
	}
}

// Approve activates a pending grant with the (possibly narrowed) scopes and TTL.
func (s *Service) Approve(ctx context.Context, id, sub string, scopes []string, ttl time.Duration) (*Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if g.Status != "pending" {
		return nil, fmt.Errorf("grant is %s", g.Status)
	}
	if s.Now().Sub(g.CreatedAt) > s.Pol.Defaults.PendingExpiry {
		return nil, errors.New("request expired")
	}
	if len(scopes) == 0 {
		return nil, errors.New("no scopes selected")
	}
	for _, sc := range scopes {
		if !slices.Contains(g.Scopes, sc) {
			return nil, fmt.Errorf("scope %q was not requested", sc)
		}
	}
	if ttl <= 0 || ttl > s.MaxTTL(scopes) {
		return nil, fmt.Errorf("ttl must be between 1s and %s", s.MaxTTL(scopes))
	}
	d := &Delivery{Token: rnd(), Minted: map[string]any{}}
	var revokers []func(context.Context) error
	var handles []storedHandle
	for _, sc := range scopes {
		conn, c, sp, _ := s.Pol.Scope(sc)
		if c.Kind != "mint" {
			continue
		}
		m, ok := s.Minters[c.Minter]
		if !ok {
			return nil, fmt.Errorf("no minter %q configured", c.Minter)
		}
		res, err := m.Mint(ctx, mint.Request{Params: c.Params, ScopeParams: sp.Params, TTL: ttl, CredsDir: s.CredsDir})
		if err != nil {
			s.audit(g, "mint", "failed "+sc+": "+err.Error())
			return nil, fmt.Errorf("mint %s: %w", conn, err)
		}
		d.Minted[sc] = res.Value
		if res.Revoke != nil {
			revokers = append(revokers, res.Revoke)
			if res.Handle != nil {
				handles = append(handles, storedHandle{Minter: c.Minter, Handle: res.Handle})
			}
		}
		s.audit(g, "mint", sc)
	}
	now := s.Now()
	sj, _ := json.Marshal(scopes)
	var hj any
	if len(handles) > 0 {
		b, _ := json.Marshal(handles)
		hj = string(b)
	}
	_, err = s.St.DB.Exec(`UPDATE grants SET status='active',scopes_json=?,granted_ttl=?,approved_at=?,expires_at=?,approver_sub=?,token_hash=?,mint_handles_json=? WHERE id=? AND status='pending'`,
		string(sj), int64(ttl.Seconds()), now.Unix(), now.Add(ttl).Unix(), sub, hash(d.Token), hj, id)
	if err != nil {
		return nil, err
	}
	s.delivery[id], s.revokers[id] = d, revokers
	if len(scopes) < len(g.Scopes) {
		s.audit(g, "narrow", strings.Join(scopes, ","))
	}
	s.audit(g, "approve", sub)
	return s.Get(id)
}

func (s *Service) Deny(id string) error {
	return s.end(id, "denied", "deny", `status='pending'`)
}

// Release ends the agent's own grant.
func (s *Service) Release(agent, id string) error {
	g, err := s.Get(id)
	if err != nil || g.Agent != agent {
		return errors.New("no such grant")
	}
	return s.end(id, "released", "release", `status IN ('pending','active')`)
}

// Revoke ends a grant and forgets any remembered consent for its agent and scopes.
func (s *Service) Revoke(id string) error {
	g, gerr := s.Get(id)
	err := s.end(id, "revoked", "revoke", `status IN ('pending','active')`)
	if gerr == nil {
		s.forget(g.Agent, g.Scopes)
	}
	return err
}

func (s *Service) RevokeAll() (int, error) {
	gs, err := s.list(`status IN ('pending','active')`)
	s.St.DB.Exec(`DELETE FROM remembered`)
	for _, g := range gs {
		s.end(g.ID, "revoked", "revoke", `status IN ('pending','active')`)
	}
	return len(gs), err
}

func (s *Service) end(id, status, event, cond string) error {
	s.mu.Lock()
	res, err := s.St.DB.Exec(`UPDATE grants SET status=?, expires_at=? WHERE id=? AND `+cond, status, s.Now().Unix(), id)
	var n int64
	if err == nil {
		n, _ = res.RowsAffected()
	}
	revokers := s.revokers[id]
	delete(s.revokers, id)
	delete(s.delivery, id)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("grant not in a state that allows this")
	}
	g, _ := s.Get(id)
	s.audit(g, event, "")
	s.runRevokers(g, revokers)
	s.St.DB.Exec(`UPDATE grants SET mint_handles_json=NULL WHERE id=?`, id)
	return nil
}

// Recover re-arms upstream revocation after a restart: handles of still-active grants are
// restored for later, handles of grants that already ended are revoked now.
func (s *Service) Recover() {
	rows, err := s.St.DB.Query(`SELECT id,status,COALESCE(expires_at,0),mint_handles_json FROM grants WHERE mint_handles_json IS NOT NULL AND mint_handles_json!=''`)
	if err != nil {
		return
	}
	type row struct {
		id, status, hj string
		exp            int64
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.status, &r.exp, &r.hj) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	for _, r := range all {
		var hs []storedHandle
		json.Unmarshal([]byte(r.hj), &hs)
		var rs []func(context.Context) error
		for _, h := range hs {
			if rest, ok := s.Minters[h.Minter].(mint.Restorer); ok {
				rs = append(rs, rest.Restore(h.Handle))
			}
		}
		if r.status == "active" && r.exp > s.Now().Unix() {
			s.mu.Lock()
			s.revokers[r.id] = rs
			s.mu.Unlock()
			continue
		}
		g, _ := s.Get(r.id)
		s.runRevokers(g, rs)
		s.St.DB.Exec(`UPDATE grants SET mint_handles_json=NULL WHERE id=?`, r.id)
	}
}

func (s *Service) runRevokers(g *Grant, rs []func(context.Context) error) {
	for _, r := range rs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := r(ctx)
		cancel()
		d := "ok"
		if err != nil {
			d = err.Error()
		}
		s.audit(g, "revoke_upstream", d)
	}
}

// Lookup resolves a grant token to an active grant.
func (s *Service) Lookup(token string) (*Grant, error) {
	g, err := scan(s.St.DB.QueryRow(`SELECT `+cols+` FROM grants WHERE token_hash=? AND status='active' AND expires_at>?`, hash(token), s.Now().Unix()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("invalid or expired grant token")
	}
	return g, err
}

// Wait polls until the grant leaves "pending" or the timeout passes. The secret
// delivery is returned at most once.
func (s *Service) Wait(ctx context.Context, agent, id string, timeout time.Duration) (*Grant, *Delivery, error) {
	deadline := time.Now().Add(timeout)
	for {
		g, err := s.Get(id)
		if err != nil || g.Agent != agent {
			return nil, nil, errors.New("no such grant")
		}
		if g.Status != "pending" || time.Now().After(deadline) {
			s.mu.Lock()
			d := s.delivery[id]
			delete(s.delivery, id)
			s.mu.Unlock()
			return g, d, nil
		}
		select {
		case <-ctx.Done():
			return g, nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Reap expires old grants and runs upstream revocation. Called every 15s.
func (s *Service) Reap() {
	now := s.Now().Unix()
	exp, _ := s.list(`(status='active' AND expires_at<=?) OR (status='pending' AND created_at<=?)`, now, now-int64(s.Pol.Defaults.PendingExpiry.Seconds()))
	for _, g := range exp {
		s.end(g.ID, "expired", "expire", `status IN ('pending','active')`)
	}
}

func (s *Service) RunReaper(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Reap()
		}
	}
}
