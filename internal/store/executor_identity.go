package store

import (
	"encoding/json"
	"errors"

	"mewdex/internal/hostidentity"
	"mewdex/internal/protocol"
)

// BindExecutorHost upgrades an existing credential on its first verified node
// handshake. The transaction also prevents two profiles from claiming one host.
func (s *Store) BindExecutorHost(identityID, deviceID, hostID string) error {
	if !hostidentity.Valid(hostID) {
		return errors.New("a stable host identity is required; update this execution service")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM identities WHERE json_extract(data,'$.id')=?", identityID).Scan(&raw); err != nil {
		return errors.New("executor identity was revoked")
	}
	var identity protocol.Identity
	if err = json.Unmarshal(raw, &identity); err != nil {
		return err
	}
	if identity.Role != "executor" || identity.DeviceID != deviceID || identity.HostID != "" && identity.HostID != hostID {
		return errors.New("executor credential does not match this device and host")
	}
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM identities WHERE json_extract(data,'$.role')='executor' AND json_extract(data,'$.hostId')=? AND json_extract(data,'$.id')<>?", hostID, identityID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("this host already has an executor in this Hub; reuse its existing installation")
	}
	identity.HostID = hostID
	if _, err = tx.Exec("UPDATE identities SET data=? WHERE json_extract(data,'$.id')=?", protocol.JSON(identity), identityID); err != nil {
		return err
	}
	return tx.Commit()
}
