package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"mewdex/internal/config"
	"mewdex/internal/hostidentity"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
)

// An invitation is submitted once. An accepted credential is kept here before
// replacing the active file, so interrupted adoption can resume without pairing
// twice. A lost response needs a different, explicitly supplied invitation.
type executorPairingAttempt struct {
	Format         int          `json:"format"`
	ID             string       `json:"id"`
	HubURL         string       `json:"hubUrl"`
	DeviceID       string       `json:"deviceId"`
	PreviousSHA256 string       `json:"previousSha256"`
	InvitationHash string       `json:"invitationHash"`
	Credentials    *Credentials `json:"credentials,omitempty"`
}

type executorAuthorization struct {
	Format           int    `json:"format"`
	HubURL           string `json:"hubUrl"`
	DeviceID         string `json:"deviceId"`
	CredentialSHA256 string `json:"credentialSha256"`
	Rejected         bool   `json:"rejected"`
}

type pairingFiles struct {
	root  *os.Root
	token string
}

func (m *Manager) executorPairingFiles(c config.Config) (*pairingFiles, error) {
	relative, err := m.ownedRelative(c.NodeTokenFile)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(m.options.Root)
	if err != nil {
		return nil, err
	}
	return &pairingFiles{root: root, token: relative}, nil
}

func credentialHash(raw []byte) string {
	if raw == nil {
		return "" // A missing credential differs from an empty file.
	}
	return hashBytes(raw)
}

func (f *pairingFiles) read(path string) ([]byte, error) {
	info, err := f.root.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("executor pairing state must be a bounded regular file")
	}
	file, err := f.root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if len(raw) > 64<<10 {
		return nil, errors.New("executor pairing state is too large")
	}
	return raw, err
}

func (f *pairingFiles) create(path string, raw []byte) error {
	file, err := f.root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(raw); err != nil {
		return err
	}
	return file.Sync()
}

func (f *pairingFiles) replace(path string, before, after []byte) error {
	if before == nil {
		return f.create(path, after)
	}
	temporary := path + "." + protocol.ID() + ".tmp"
	if err := f.create(temporary, after); err != nil {
		return err
	}
	defer f.root.Remove(temporary)
	current, err := f.read(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("executor pairing state changed; its files were preserved")
	}
	return f.root.Rename(temporary, path)
}

func (f *pairingFiles) authorization(c config.Config, token []byte, rejected bool) error {
	path := f.token + ".authorization.json"
	before, err := f.read(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return f.replace(path, before, protocol.JSON(executorAuthorization{Format: 1, HubURL: c.HubURL, DeviceID: c.DeviceID, CredentialSHA256: credentialHash(token), Rejected: rejected}))
}

func (m *Manager) executorNeedsPairing(c config.Config) bool {
	f, err := m.executorPairingFiles(c)
	if err != nil {
		return true
	}
	defer f.root.Close()
	token, err := f.read(f.token)
	if err != nil {
		return true
	}
	raw, err := f.read(f.token + ".authorization.json")
	var state executorAuthorization
	return err == nil && json.Unmarshal(raw, &state) == nil && state.Format == 1 && state.Rejected && state.HubURL == c.HubURL && state.DeviceID == c.DeviceID && state.CredentialSHA256 == credentialHash(token)
}

func (m *Manager) executorIdentity(ctx context.Context, c config.Config, token string) (protocol.Identity, error) {
	var identity protocol.Identity
	err := m.api(ctx, c.HubURL, "/v1/me", nil, strings.TrimSpace(token), &identity)
	if err == nil && (identity.Role != "executor" || identity.DeviceID != c.DeviceID) {
		err = errors.New("credential does not match this executor; its files were preserved")
	}
	if err == nil && identity.HostID != "" {
		host, hostErr := hostidentity.ID(ctx)
		if hostErr != nil {
			return identity, hostErr
		}
		if identity.HostID != host {
			err = errors.New("executor credential belongs to another host; its files were preserved")
		}
	}
	return identity, err
}

func validExecutorCredentials(c config.Config, value *Credentials) bool {
	return value != nil && value.ProtocolVersion == protocol.Version && value.Identity.Role == "executor" && value.Identity.DeviceID == c.DeviceID && strings.TrimSpace(value.Token) != ""
}

func (m *Manager) pairExecutor(ctx context.Context, c config.Config, invitation string) error {
	hostID, err := hostidentity.ID(ctx)
	if err != nil {
		return err
	}
	f, err := m.executorPairingFiles(c)
	if err != nil {
		return err
	}
	defer f.root.Close()
	invitation = strings.TrimSpace(invitation)
	token, err := f.read(f.token)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if token != nil {
		_, checkErr := m.executorIdentity(ctx, c, string(token))
		if checkErr == nil {
			if err = f.finishAdopted(c, token); err != nil {
				return err
			}
			if err = f.authorization(c, token, false); err != nil {
				return err
			}
			return nil // Never consume an invitation for a valid identity.
		}
		if !isHubUnauthorized(checkErr) {
			return checkErr // Offline, forbidden, mismatch and server errors cannot justify replacement.
		}
		if err = f.authorization(c, token, true); err != nil {
			return err
		}
	}
	path := f.token + ".pairing.json"
	before, err := f.read(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var attempt executorPairingAttempt
	if before != nil {
		if json.Unmarshal(before, &attempt) != nil || attempt.Format != 1 || !localID(attempt.ID) || attempt.HubURL != c.HubURL || attempt.DeviceID != c.DeviceID || attempt.PreviousSHA256 != credentialHash(token) {
			return errors.New("saved executor pairing does not match the current state; its files were preserved")
		}
		if validExecutorCredentials(c, attempt.Credentials) {
			identity, checkErr := m.executorIdentity(ctx, c, attempt.Credentials.Token)
			if checkErr == nil {
				return f.adopt(c, attempt, before, token, identity)
			}
			if !isHubUnauthorized(checkErr) {
				return checkErr
			}
		}
		if invitation == "" || credentialHash([]byte(invitation)) == attempt.InvitationHash {
			return errors.New("executor pairing outcome is unconfirmed; inspect Hub membership before supplying a different invitation; existing credentials were preserved")
		}
		// A different explicit invitation starts another attempt. Keep the previous
		// attempt for inspection, including any credential whose adoption failed.
		archive := path + ".unconfirmed." + attempt.ID
		if _, err = f.root.Lstat(archive); !errors.Is(err, os.ErrNotExist) {
			return errors.New("previous executor pairing record could not be preserved")
		}
		if err = f.root.Rename(path, archive); err != nil {
			return err
		}
	}
	if invitation == "" {
		return errors.New("a new executor invitation is required; rejected credentials were preserved")
	}
	attempt = executorPairingAttempt{Format: 1, ID: protocol.ID(), HubURL: c.HubURL, DeviceID: c.DeviceID, PreviousSHA256: credentialHash(token), InvitationHash: credentialHash([]byte(invitation))}
	before = protocol.JSON(attempt)
	if err = f.create(path, before); err != nil {
		return err
	}
	var credentials Credentials
	if err = m.api(ctx, c.HubURL, "/v1/pair", map[string]string{"code": invitation, "name": c.Name, "deviceId": c.DeviceID, "hostId": hostID}, "", &credentials); err != nil {
		return errors.New("executor pairing response is unconfirmed; existing credentials were preserved; inspect Hub membership before using a different invitation")
	}
	attempt.Credentials = &credentials
	after := protocol.JSON(attempt)
	if err = f.replace(path, before, after); err != nil {
		return errors.New("executor was paired but its result could not be saved; inspect network membership before using a different invitation")
	}
	if !validExecutorCredentials(c, &credentials) {
		return errors.New("invitation did not return this executor's identity; existing credentials were preserved")
	}
	identity, err := m.executorIdentity(ctx, c, credentials.Token)
	if err != nil {
		return err // The saved result can be checked again without submitting the invitation.
	}
	return f.adopt(c, attempt, after, token, identity)
}

func (f *pairingFiles) finishAdopted(c config.Config, token []byte) error {
	path := f.token + ".pairing.json"
	raw, err := f.read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var attempt executorPairingAttempt
	if json.Unmarshal(raw, &attempt) != nil || attempt.Format != 1 {
		return errors.New("saved executor pairing cannot be read; its files were preserved")
	}
	if attempt.HubURL == c.HubURL && attempt.DeviceID == c.DeviceID && validExecutorCredentials(c, attempt.Credentials) && bytes.Equal(token, []byte(attempt.Credentials.Token)) {
		return f.root.Remove(path) // Replacement finished before its process exited.
	}
	return nil // A valid existing identity never consumes or overwrites another result.
}

func (f *pairingFiles) adopt(c config.Config, attempt executorPairingAttempt, record, previous []byte, identity protocol.Identity) error {
	if previous != nil {
		backup := f.token + ".rejected." + attempt.ID
		retained, err := f.read(backup)
		if errors.Is(err, os.ErrNotExist) {
			err = f.create(backup, previous)
		} else if err == nil && !bytes.Equal(retained, previous) {
			err = errors.New("preserved executor credential differs from the previous file")
		}
		if err != nil {
			return err
		}
	}
	if err := f.replace(f.token, previous, []byte(attempt.Credentials.Token)); err != nil {
		return err // Keep the saved result and original credential for a later observation.
	}
	if err := f.authorization(c, []byte(attempt.Credentials.Token), false); err != nil {
		return err
	}
	// Clear only this fully adopted journal. Its token is already in node-token.
	current, err := f.read(f.token + ".pairing.json")
	if err != nil || !bytes.Equal(current, record) {
		return errors.New("executor credential was adopted but its pairing record changed; inspect local state")
	}
	if err = f.root.Remove(f.token + ".pairing.json"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) repairExecutor(ctx context.Context, request *PairingRequest) error {
	if request == nil {
		return errors.New("executor pairing settings are required")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		return err
	}
	if c.Hub || !c.Executor || c.NodeTokenFile == "" {
		return errors.New("this installation is not a remote executor requiring pairing")
	}
	if _, err = m.ownedRelative(c.NodeTokenFile); err != nil {
		return err
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return errors.New("stop the local executor before repairing its pairing")
	}
	defer lock.Close()
	return m.pairExecutor(ctx, c, request.Invitation)
}
