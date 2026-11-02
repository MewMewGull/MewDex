package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestExistingIdentityMigrationPreservesInputAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_version(version INTEGER);INSERT INTO schema_version VALUES(1);CREATE TABLE identities(hash TEXT PRIMARY KEY,data BLOB NOT NULL);CREATE TABLE invitations(hash TEXT PRIMARY KEY,role TEXT NOT NULL,expires INTEGER NOT NULL);INSERT INTO identities VALUES(?,?)`, Hash("old-client"), `{"id":"client","name":"existing","role":"client","createdAt":"before"}`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Authenticate("old-client")
	if err != nil {
		t.Fatal(err)
	}
	if !id.Access.AllDevices || id.Access.Gateway {
		t.Fatalf("migration changed existing input access: %+v", id.Access)
	}
}
