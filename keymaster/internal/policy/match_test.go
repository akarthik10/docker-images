package policy

import "testing"

func TestCleanPath(t *testing.T) {
	for _, bad := range []string{"/a/../b", "/a/%2e%2e/b", "/a%2fb", "/a//b", "/a/./b", "x", "/a%00", "/a\\b"} {
		if _, ok := CleanPath(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
	for _, good := range []string{"/", "/a/b", "/a/b/", "/a%20b"} {
		if _, ok := CleanPath(good); !ok {
			t.Errorf("%q should be accepted", good)
		}
	}
}

func TestAllows(t *testing.T) {
	s := Scope{Rules: []Rule{{Methods: []string{"GET"}, Paths: []string{"^/Notes(/.*)?$"}}}}
	if !s.Allows("get", "/Notes/x") || s.Allows("PUT", "/Notes/x") || s.Allows("GET", "/Notes2") || s.Allows("GET", "/other") {
		t.Fatal("rule matching wrong")
	}
}
