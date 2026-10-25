package policy

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// CleanPath validates an escaped request path and returns the decoded form.
// It refuses anything that could make the rule see a different path than the upstream.
func CleanPath(escaped string) (string, bool) {
	low := strings.ToLower(escaped)
	for _, bad := range []string{"%2f", "%5c", "%2e", "%00", "\\"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	p, err := url.PathUnescape(escaped)
	if err != nil || strings.Contains(p, "\x00") || !strings.HasPrefix(p, "/") {
		return "", false
	}
	if path.Clean(p) != strings.TrimSuffix(p, "/") && p != "/" {
		return "", false
	}
	return p, true
}

// Allows reports whether any rule of the scope permits method + (already cleaned) path.
func (s Scope) Allows(method, p string) bool {
	for _, r := range s.Rules {
		okM := false
		for _, m := range r.Methods {
			if strings.EqualFold(m, method) {
				okM = true
			}
		}
		if !okM {
			continue
		}
		for _, re := range r.Paths {
			if regexp.MustCompile(re).MatchString(p) { // validated at load
				return true
			}
		}
	}
	return false
}
