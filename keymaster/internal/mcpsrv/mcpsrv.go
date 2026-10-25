// Package mcpsrv is the agent-facing control plane (MCP over streamable HTTP, stateless).
package mcpsrv

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/akarthik10/keymaster/internal/agentauth"
	"github.com/akarthik10/keymaster/internal/grants"
)

type Server struct {
	G         *grants.Service
	Auth      agentauth.Authenticator
	PublicURL string        // for approval links
	ProxyURL  string        // how the agent reaches the proxy, e.g. http://keymaster.default.svc:8080
	Wait      time.Duration // max wait in request_access / await_grant (keep under the MCP tool timeout)
}

type empty struct{}

type ScopeInfo struct {
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	MaxTTL string `json:"max_ttl"`
	Write  bool   `json:"write"`
}
type ConnectorInfo struct {
	Name     string      `json:"name"`
	Kind     string      `json:"kind"`
	ProxyURL string      `json:"proxy_url,omitempty"`
	Scopes   []ScopeInfo `json:"scopes"`
}
type ConnectorsOut struct {
	Connectors []ConnectorInfo `json:"connectors"`
	Usage      string          `json:"usage"`
}

type RequestIn struct {
	Scopes []string `json:"scopes" jsonschema:"scopes as connector:scope, e.g. copyparty:notes-read"`
	Reason string   `json:"reason" jsonschema:"why access is needed (shown to the approver, unverified)"`
	TTL    string   `json:"ttl,omitempty" jsonschema:"requested duration like 15m; capped by policy"`
}
type GrantOut struct {
	GrantID     string         `json:"grant_id"`
	Status      string         `json:"status"`
	ApprovalURL string         `json:"approval_url,omitempty"`
	GrantToken  string         `json:"grant_token,omitempty"`
	ExpiresAt   string         `json:"expires_at,omitempty"`
	Scopes      []string       `json:"granted_scopes,omitempty"`
	Minted      map[string]any `json:"minted,omitempty"`
	Note        string         `json:"note,omitempty"`
}
type AwaitIn struct {
	GrantID string `json:"grant_id"`
}
type GrantsOut struct {
	Grants []GrantOut `json:"grants"`
}

func (s *Server) connectors(agent string) ConnectorsOut {
	out := ConnectorsOut{Usage: "Call request_access, give the approval_url to the user, then await_grant. " +
		"For proxy connectors send requests to proxy_url with 'Authorization: Bearer <grant_token>'. " +
		"Call release when done."}
	for name, c := range s.G.Pol.Connectors {
		if !s.G.Pol.AgentAllows(agent, name) {
			continue
		}
		ci := ConnectorInfo{Name: name, Kind: c.Kind}
		if c.Kind == "proxy" {
			ci.ProxyURL = s.ProxyURL + "/p/" + name
		}
		for sn, sp := range c.Scopes {
			if sp.Tier < 3 {
				ci.Scopes = append(ci.Scopes, ScopeInfo{name + ":" + sn, sp.Tier, sp.MaxTTL.String(), c.IsWrite(sp)})
			}
		}
		sort.Slice(ci.Scopes, func(i, j int) bool { return ci.Scopes[i].Name < ci.Scopes[j].Name })
		out.Connectors = append(out.Connectors, ci)
	}
	sort.Slice(out.Connectors, func(i, j int) bool { return out.Connectors[i].Name < out.Connectors[j].Name })
	return out
}

func (s *Server) view(g *grants.Grant, d *grants.Delivery) GrantOut {
	o := GrantOut{GrantID: g.ID, Status: g.Status}
	switch g.Status {
	case "pending":
		o.ApprovalURL = s.PublicURL + "/approve/" + g.ID
		o.Note = "ACTION REQUIRED: print this exact line to the user in your reply now: \"Approve access: " + o.ApprovalURL + "\". Then call await_grant."
	case "active":
		o.ExpiresAt = g.ExpiresAt.UTC().Format(time.RFC3339)
		o.Scopes = g.Scopes
		if d != nil {
			o.GrantToken, o.Minted = d.Token, d.Minted
		} else {
			o.Note = "Secrets were already delivered once and cannot be shown again."
		}
	}
	return o
}

func (s *Server) wait(ctx context.Context, agent, id string) (GrantOut, error) {
	g, d, err := s.G.Wait(ctx, agent, id, s.Wait)
	if err != nil {
		return GrantOut{}, err
	}
	return s.view(g, d), nil
}

// build makes a server bound to one authenticated agent.
func (s *Server) build(agent string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "keymaster", Version: "0.1"}, nil)

	mcp.AddTool(srv, &mcp.Tool{Name: "list_connectors", Description: "List the connectors and scopes you can request access to."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, ConnectorsOut, error) {
			return nil, s.connectors(agent), nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "request_access", Description: "Ask the user for scoped, time-boxed access. If status is pending, print the approval_url to the user in chat, then call await_grant."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in RequestIn) (*mcp.CallToolResult, GrantOut, error) {
			ttl, _ := time.ParseDuration(in.TTL)
			g, err := s.G.Request(ctx, agent, in.Scopes, in.Reason, ttl)
			if err != nil {
				return nil, GrantOut{}, err
			}
			out, err := s.wait(ctx, agent, g.ID)
			return nil, out, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "await_grant", Description: "Wait for a pending grant to be approved."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in AwaitIn) (*mcp.CallToolResult, GrantOut, error) {
			out, err := s.wait(ctx, agent, in.GrantID)
			return nil, out, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "list_grants", Description: "List your pending and active grants (no secrets)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, GrantsOut, error) {
			gs, err := s.G.ListAgent(agent)
			out := GrantsOut{Grants: []GrantOut{}}
			for _, g := range gs {
				v := s.view(g, nil)
				v.Note = ""
				out.Grants = append(out.Grants, v)
			}
			return nil, out, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "release", Description: "End a grant now. Do this when your task is done."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in AwaitIn) (*mcp.CallToolResult, GrantOut, error) {
			if err := s.G.Release(agent, in.GrantID); err != nil {
				return nil, GrantOut{}, err
			}
			return nil, GrantOut{GrantID: in.GrantID, Status: "released"}, nil
		})
	return srv
}

// Handler authenticates the agent per request and serves MCP statelessly.
func (s *Server) Handler() http.Handler {
	mh := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		agent, err := s.Auth.Agent(r)
		if err != nil {
			return nil
		}
		return s.build(agent)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Auth.Agent(r); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mh.ServeHTTP(w, r)
	})
}
