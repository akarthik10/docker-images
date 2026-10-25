// Package store is the SQLite persistence layer (grants + audit).
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS grants (
  id TEXT PRIMARY KEY,
  agent TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','active','denied','expired','released','revoked')),
  scopes_json TEXT NOT NULL,
  requested_ttl INTEGER NOT NULL,
  granted_ttl INTEGER,
  reason TEXT,
  created_at INTEGER NOT NULL,
  approved_at INTEGER,
  expires_at INTEGER,
  approver_sub TEXT,
  token_hash TEXT,
  oidc_state_hash TEXT,
  mint_handles_json TEXT
);
CREATE INDEX IF NOT EXISTS grants_token ON grants(token_hash);
CREATE TABLE IF NOT EXISTS remembered (
  agent TEXT NOT NULL,
  scope TEXT NOT NULL,
  approver_sub TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (agent, scope)
);
CREATE TABLE IF NOT EXISTS audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  grant_id TEXT,
  agent TEXT,
  event TEXT NOT NULL,
  connector TEXT,
  method TEXT,
  path TEXT,
  status INTEGER,
  bytes INTEGER,
  detail TEXT
);
`

type Store struct{ DB *sql.DB }

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer keeps SQLite simple
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// Audit records an event. Metadata only, never bodies.
func (s *Store) Audit(ts int64, grantID, agent, event, connector, method, path string, status, bytes int, detail string) error {
	_, err := s.DB.Exec(`INSERT INTO audit(ts,grant_id,agent,event,connector,method,path,status,bytes,detail)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, ts, grantID, agent, event, connector, method, path, status, bytes, detail)
	return err
}
