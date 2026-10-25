// Package policy loads and validates the connector/scope policy file.
package policy

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Policy struct {
	Agents     []Agent              `yaml:"agents"`
	Connectors map[string]Connector `yaml:"connectors"`
	Defaults   Defaults             `yaml:"defaults"`
}

type Agent struct {
	SA                string   `yaml:"sa"` // namespace/name
	AllowedConnectors []string `yaml:"allowed_connectors"`
}

type Connector struct {
	Kind             string            `yaml:"kind"` // proxy | mint
	Upstream         string            `yaml:"upstream"`
	Auth             map[string]string `yaml:"auth"`
	Minter           string            `yaml:"minter"`
	Params           map[string]any    `yaml:"params"`
	UntrustedContent bool              `yaml:"untrusted_content"`
	Scopes           map[string]Scope  `yaml:"scopes"`
}

type Scope struct {
	Tier   int            `yaml:"tier"`
	MaxTTL time.Duration  `yaml:"max_ttl"`
	Rules  []Rule         `yaml:"rules"`
	Params map[string]any `yaml:"params"`
}

type Rule struct {
	Methods []string `yaml:"methods"`
	Paths   []string `yaml:"paths"` // regexes, anchored by the author
}

type Defaults struct {
	ProxyTTL      time.Duration `yaml:"proxy_ttl"`
	PendingExpiry time.Duration `yaml:"pending_expiry"`
	AuthMaxAge    time.Duration `yaml:"auth_max_age"`
}

// Load reads and validates the policy file. Any problem is an error (fail closed).
func Load(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Policy, error) {
	var p Policy
	dec := yaml.NewDecoder(bytesReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	if p.Defaults.ProxyTTL == 0 {
		p.Defaults.ProxyTTL = 15 * time.Minute
	}
	if p.Defaults.PendingExpiry == 0 {
		p.Defaults.PendingExpiry = 10 * time.Minute
	}
	if p.Defaults.AuthMaxAge == 0 {
		p.Defaults.AuthMaxAge = 120 * time.Second
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	return &p, nil
}

func (p *Policy) validate() error {
	for _, a := range p.Agents {
		if a.SA == "" {
			return fmt.Errorf("agent with empty sa")
		}
		for _, c := range a.AllowedConnectors {
			if _, ok := p.Connectors[c]; !ok {
				return fmt.Errorf("agent %s: unknown connector %q", a.SA, c)
			}
		}
	}
	for name, c := range p.Connectors {
		switch c.Kind {
		case "proxy":
			if c.Upstream == "" {
				return fmt.Errorf("connector %s: proxy needs upstream", name)
			}
		case "mint":
			if c.Minter == "" {
				return fmt.Errorf("connector %s: mint needs minter", name)
			}
		default:
			return fmt.Errorf("connector %s: kind must be proxy or mint, got %q", name, c.Kind)
		}
		if len(c.Scopes) == 0 {
			return fmt.Errorf("connector %s: no scopes", name)
		}
		for sn, s := range c.Scopes {
			if s.Tier < 0 || s.Tier > 3 {
				return fmt.Errorf("scope %s:%s: tier must be 0-3", name, sn)
			}
			if s.Tier == 3 {
				continue // never grantable, nothing else to check
			}
			if s.MaxTTL <= 0 {
				return fmt.Errorf("scope %s:%s: max_ttl required", name, sn)
			}
			if c.Kind == "proxy" {
				if len(s.Rules) == 0 {
					return fmt.Errorf("scope %s:%s: proxy scope needs rules", name, sn)
				}
				for _, r := range s.Rules {
					if len(r.Methods) == 0 || len(r.Paths) == 0 {
						return fmt.Errorf("scope %s:%s: rule needs methods and paths", name, sn)
					}
					for _, re := range r.Paths {
						if _, err := regexp.Compile(re); err != nil {
							return fmt.Errorf("scope %s:%s: bad path regex %q: %w", name, sn, re, err)
						}
					}
				}
			}
		}
	}
	return nil
}

// Scope looks up "connector:scope".
func (p *Policy) Scope(full string) (conn string, c Connector, s Scope, ok bool) {
	conn, sn, found := strings.Cut(full, ":")
	if !found {
		return
	}
	c, ok = p.Connectors[conn]
	if !ok {
		return
	}
	s, ok = c.Scopes[sn]
	return
}

// AgentAllows reports whether the agent may use the connector.
func (p *Policy) AgentAllows(agent, connector string) bool {
	for _, a := range p.Agents {
		if a.SA == agent {
			return slices.Contains(a.AllowedConnectors, connector)
		}
	}
	return false
}

// IsWrite reports whether the scope can change things.
func (c Connector) IsWrite(s Scope) bool {
	if c.Kind == "mint" {
		return true
	}
	for _, r := range s.Rules {
		for _, m := range r.Methods {
			switch strings.ToUpper(m) {
			case "GET", "HEAD", "OPTIONS", "PROPFIND":
			default:
				return true
			}
		}
	}
	return false
}
