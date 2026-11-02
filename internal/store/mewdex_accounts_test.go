package store

import (
	"encoding/json"
	"mewdex/internal/protocol"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMewDexPasswordAndKeysShareStableAccountAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := s.Invite("admin", time.Minute)
	oldKey, actor, err := s.Pair(code, "old input", "")
	if err != nil {
		t.Fatal(err)
	}
	password := "MewDex-password-fixture"
	account, err := s.SetMewDexAccount(actor, "Owner", "MewDex 用户", password)
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != actor.ID {
		t.Fatal("changed existing identity and receipt ownership")
	}
	if strings.Contains(string(protocol.JSON(account)), "password") {
		t.Fatal("public account includes password fields")
	}
	for _, name := range []string{"owner", " OWNER "} {
		key, id, e := s.LoginMewDex(name, password)
		if e != nil || id.ID != actor.ID || id.AccountID != account.ID || id.Role != "admin" || !id.Access.Allows("any-new-device", "any-project") {
			t.Fatal("wrong account scope", id, e)
		}
		if _, e = s.Authenticate(key); e != nil {
			t.Fatal(e)
		}
	}
	entries, err := s.Identities()
	if err != nil || len(entries) != 1 {
		t.Fatal("password sessions duplicated user accounts", entries, err)
	}
	if _, _, err = s.LoginMewDex("owner", "wrong-password"); err != ErrInvalidMewDexLogin {
		t.Fatal("wrong password accepted")
	}
	if _, _, err = s.LoginMewDex("unknown", password); err != ErrInvalidMewDexLogin {
		t.Fatal("unknown user accepted")
	}
	old, err := s.Authenticate(oldKey)
	if err != nil || old.AccountID != account.ID {
		t.Fatal("old key lost", err)
	}
	records, _ := s.List("mewdex_accounts")
	if len(records) != 1 || strings.Contains(string(records[0]), password) {
		t.Fatal("raw password persisted")
	}
	var record mewdexAccountRecord
	_ = json.Unmarshal(records[0], &record)
	if !strings.HasPrefix(record.PasswordHash, "$2") {
		t.Fatal("password hash absent")
	}
	if _, err = s.SetMewDexAccount(old, "owner", "中央账号新名称", "changed-password-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.LoginMewDex("owner", password); err != ErrInvalidMewDexLogin {
		t.Fatal("previous password still works after changing it")
	}
	password = "changed-password-fixture"
	if _, err = s.Authenticate(oldKey); err != nil {
		t.Fatal("changing MewDex password invalidated saved key")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, id, err := s.LoginMewDex("owner", password)
	if err != nil || id.ID != actor.ID {
		t.Fatal("account not recovered", err)
	}
	if err = s.Revoke(id.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(oldKey); err == nil {
		t.Fatal("revoked key still authenticates")
	}
}
