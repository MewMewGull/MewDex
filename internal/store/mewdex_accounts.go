package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"mewdex/internal/protocol"
)

var ErrInvalidMewDexLogin = errors.New("MewDex 用户名或密码不正确")

type mewdexAccountRecord struct {
	protocol.MewDexAccount
	PasswordHash string            `json:"passwordHash"`
	Identity     protocol.Identity `json:"identity"`
}

var dummyPasswordHash = sync.OnceValue(func() []byte {
	hash, _ := bcrypt.GenerateFromPassword([]byte(protocol.ID()), bcrypt.DefaultCost)
	return hash
})

func validateMewDexAccount(username, name, password string) (string, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	name = strings.TrimSpace(name)
	if username == "" || utf8.RuneCountInString(username) > 64 || strings.ContainsFunc(username, unicode.IsSpace) || strings.ContainsFunc(username, unicode.IsControl) {
		return "", "", errors.New("MewDex 用户名需为 1–64 个字符，不能包含空白或控制字符")
	}
	if name == "" {
		name = username
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", "", errors.New("账号名称不能超过 80 个字符")
	}
	if len(password) < 8 || len(password) > 72 {
		return "", "", errors.New("MewDex 密码需为 8–72 字节")
	}
	return username, name, nil
}

// Account metadata and its password hash stay on the Hub. Existing keys retain
// their identity ID, so receipt ownership and input state survive account setup.
func (s *Store) SetMewDexAccount(actor protocol.Identity, username, name, password string) (protocol.MewDexAccount, error) {
	var account protocol.MewDexAccount
	if actor.Role != "admin" || actor.ID == "" {
		return account, errors.New("MewDex 管理权限不足")
	}
	username, name, err := validateMewDexAccount(username, name, password)
	if err != nil {
		return account, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return account, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return account, err
	}
	defer tx.Rollback()
	record := mewdexAccountRecord{MewDexAccount: protocol.MewDexAccount{ID: actor.ID, Username: username, Name: name, CreatedAt: protocol.Now()}, PasswordHash: string(hash)}
	var raw []byte
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='mewdex_accounts' AND id=?", actor.ID).Scan(&raw)
	if err == nil {
		var prior mewdexAccountRecord
		if err = json.Unmarshal(raw, &prior); err != nil {
			return account, err
		}
		record.CreatedAt = prior.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return account, err
	}
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM objects WHERE bucket='mewdex_accounts' AND id<>? AND json_extract(data,'$.username')=?", actor.ID, username).Scan(&count); err != nil {
		return account, err
	}
	if count != 0 {
		return account, errors.New("此 MewDex 用户名已存在")
	}
	actor.AccountID, actor.Name, actor.Access = actor.ID, name, protocol.DefaultAccess("admin")
	record.Identity = actor
	if _, err = tx.Exec("INSERT INTO objects VALUES('mewdex_accounts',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", actor.ID, protocol.JSON(record)); err != nil {
		return account, err
	}
	if _, err = tx.Exec("UPDATE identities SET data=? WHERE json_extract(data,'$.id')=?", protocol.JSON(actor), actor.ID); err != nil {
		return account, err
	}
	if err = tx.Commit(); err != nil {
		return account, err
	}
	return record.MewDexAccount, nil
}

func (s *Store) MewDexAccount(id string) (*protocol.MewDexAccount, error) {
	var record mewdexAccountRecord
	if err := s.Get("mewdex_accounts", id, &record); errors.Is(err, ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &record.MewDexAccount, nil
}

func (s *Store) LoginMewDex(username, password string) (string, protocol.Identity, error) {
	var record mewdexAccountRecord
	var raw []byte
	username = strings.ToLower(strings.TrimSpace(username))
	err := s.db.QueryRow("SELECT data FROM objects WHERE bucket='mewdex_accounts' AND json_extract(data,'$.username')=?", username).Scan(&raw)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", record.Identity, err
	}
	if found {
		if err = json.Unmarshal(raw, &record); err != nil {
			return "", record.Identity, err
		}
	}
	hash := []byte(record.PasswordHash)
	if !found {
		hash = dummyPasswordHash()
	}
	if err = bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil || !found {
		return "", protocol.Identity{}, ErrInvalidMewDexLogin
	}
	return s.issueMewDexKey(record.Identity)
}

func (s *Store) issueMewDexKey(identity protocol.Identity) (string, protocol.Identity, error) {
	token := protocol.ID() + protocol.ID()
	_, err := s.db.Exec("INSERT INTO identities VALUES(?,?)", Hash(token), protocol.JSON(identity))
	return token, identity, err
}

// Local OS ownership already grants Hub administration. This only creates an
// input credential; it does not log any executor into a model provider.
func (s *Store) LocalMewDexKey() (string, protocol.Identity, error) {
	var record mewdexAccountRecord
	var raw []byte
	err := s.db.QueryRow("SELECT data FROM objects WHERE bucket='mewdex_accounts' ORDER BY json_extract(data,'$.createdAt'),id LIMIT 1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", record.Identity, ErrNotFound
	}
	if err != nil {
		return "", record.Identity, err
	}
	if err = json.Unmarshal(raw, &record); err != nil {
		return "", record.Identity, err
	}
	return s.issueMewDexKey(record.Identity)
}

func (s *Store) ProvisionMewDexAccount(login protocol.MewDexAccountLogin) error {
	accounts, err := s.List("mewdex_accounts")
	if err != nil {
		return err
	}
	if len(accounts) > 0 {
		// A pending first setup may be resumed, without resetting an existing account.
		var record mewdexAccountRecord
		if len(accounts) != 1 || json.Unmarshal(accounts[0], &record) != nil || record.Username != strings.ToLower(strings.TrimSpace(login.Username)) || bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(login.Password)) != nil {
			return errors.New("已有 MewDex 账号，原账号保持")
		}
		return nil
	}
	_, err = s.SetMewDexAccount(protocol.Identity{ID: protocol.ID(), Name: login.Username, Role: "admin", CreatedAt: protocol.Now(), Access: protocol.DefaultAccess("admin")}, login.Username, "", login.Password)
	return err
}
