package store

import (
	"path/filepath"
	"testing"
)

func TestOpenAndAudit(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "km.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Audit(1, "", "", "policy_load", "", "", "", 0, 0, "ok"); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow(`SELECT count(*) FROM audit`).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 audit row, got %d", n)
	}
	// bad status is rejected by the CHECK constraint
	if _, err := s.DB.Exec(`INSERT INTO grants(id,agent,status,scopes_json,requested_ttl,created_at) VALUES('x','a','bogus','[]',1,1)`); err == nil {
		t.Fatal("expected CHECK failure")
	}
}
