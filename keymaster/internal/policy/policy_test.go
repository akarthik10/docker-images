package policy

import (
	"strings"
	"testing"
)

const good = `
agents:
  - sa: default/claude-agent
    allowed_connectors: [cp]
connectors:
  cp:
    kind: proxy
    upstream: http://cp:3923
    scopes:
      read: {tier: 1, max_ttl: 72h, rules: [{methods: [GET], paths: ["^/a(/.*)?$"]}]}
      never: {tier: 3}
  k8s:
    kind: mint
    minter: k8s_tokenrequest
    scopes:
      write: {tier: 2, max_ttl: 10m}
`

func TestGood(t *testing.T) {
	p, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if p.Defaults.ProxyTTL.Minutes() != 15 {
		t.Fatalf("default proxy ttl not applied: %v", p.Defaults.ProxyTTL)
	}
}

func TestBad(t *testing.T) {
	cases := map[string]string{
		"proxy scope without rules": strings.Replace(good, `rules: [{methods: [GET], paths: ["^/a(/.*)?$"]}]`, `rules: []`, 1),
		"unknown connector":         strings.Replace(good, "[cp]", "[nope]", 1),
		"bad regex":                 strings.Replace(good, `"^/a(/.*)?$"`, `"(["`, 1),
		"bad kind":                  strings.Replace(good, "kind: mint", "kind: wat", 1),
		"unknown field":             good + "bogus: 1\n",
		"missing max_ttl":           strings.Replace(good, "max_ttl: 10m", "max_ttl: 0s", 1),
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
