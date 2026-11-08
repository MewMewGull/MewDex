// Package startup manages only the operating-system entry owned by one MewDex
// installation. It never stores model credentials or changes device data.
package startup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/protocol"
)

type Binding struct {
	DeviceID      string `json:"deviceId"`
	ProfileSHA256 string `json:"profileSha256"`
}

type Definition struct {
	Name      string   `json:"name"`
	Owner     string   `json:"owner"`
	Scope     string   `json:"scope"`
	Root      string   `json:"root"`
	Program   string   `json:"program"`
	Arguments []string `json:"arguments"`
}

type Entry struct {
	Present bool
	Owner   string
	Enabled bool
	Running bool
	Matches bool
	Raw     []byte
}

type Backend interface {
	Scope() string
	Inspect(context.Context, Definition) (Entry, error)
	Install(context.Context, Definition) error
	Remove(context.Context, Definition) error
	Start(context.Context, Definition) error
	Stop(context.Context, Definition) error
}

type recoveryBackend interface {
	recoverySession() (string, error)
}

type Record struct {
	Format        int        `json:"format"`
	Definition    Definition `json:"definition"`
	Binding       Binding    `json:"binding"`
	Enabled       bool       `json:"enabled"`
	Pending       bool       `json:"pending"`
	UpdatedAt     string     `json:"updatedAt"`
	PausedSession string     `json:"pausedSession,omitempty"`
}

type Info struct {
	Supported        bool   `json:"supported"`
	Registered       bool   `json:"registered"`
	Enabled          bool   `json:"enabled"`
	RequestedEnabled bool   `json:"requestedEnabled"`
	EntryMatches     bool   `json:"entryMatches"`
	MatchesProfile   bool   `json:"matchesProfile"`
	Running          bool   `json:"running"`
	Pending          bool   `json:"pending"`
	Scope            string `json:"scope"`
	Name             string `json:"name"`
	Message          string `json:"message,omitempty"`
}

type Controller struct {
	root    string
	backend Backend
}

func New(root string, backend Backend) (*Controller, error) {
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\r\n\x00") || backend == nil {
		return nil, errors.New("startup requires an absolute installation directory and platform backend")
	}
	return &Controller{root: filepath.Clean(root), backend: backend}, nil
}

func Name(root string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	return "app.mewdex.device." + hex.EncodeToString(digest[:12])
}

func Fingerprint(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (c *Controller) definition(program, owner string) Definition {
	return Definition{Name: Name(c.root) + "." + owner[:16], Owner: owner, Scope: c.backend.Scope(), Root: c.root, Program: program, Arguments: []string{"managed", "-root", c.root, "-owner", owner}}
}

func (c *Controller) ownsProgram(program string) bool {
	if !filepath.IsAbs(program) || strings.ContainsAny(program, "\r\n\x00") {
		return false
	}
	relative, err := filepath.Rel(c.root, program)
	return err == nil && relative != "." && filepath.IsLocal(relative)
}

func (c *Controller) read() (Record, []byte, error) {
	var record Record
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return record, nil, err
	}
	defer root.Close()
	info, err := root.Lstat("startup.json")
	if err != nil {
		return record, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return record, nil, errors.New("startup record must be a bounded regular file")
	}
	file, err := root.Open("startup.json")
	if err != nil {
		return record, nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return record, nil, errors.New("startup record cannot be read within its limit")
	}
	if json.Unmarshal(raw, &record) != nil || record.Format != 1 || len(record.Definition.Owner) != 32 || strings.Trim(record.Definition.Owner, "0123456789abcdef") != "" || record.Definition.Root != c.root || record.Definition.Scope != c.backend.Scope() {
		return record, nil, errors.New("startup record does not belong to this installation")
	}
	if record.Enabled && !c.ownsProgram(record.Definition.Program) {
		return record, nil, errors.New("startup program is outside this installation")
	}
	expected := c.definition(record.Definition.Program, record.Definition.Owner)
	if !bytes.Equal(protocol.JSON(expected), protocol.JSON(record.Definition)) {
		return record, nil, errors.New("startup command does not match this installation")
	}
	return record, raw, nil
}

func (c *Controller) save(before []byte, record Record) error {
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return err
	}
	defer root.Close()
	raw := protocol.JSON(record)
	name := "startup.json"
	if before != nil {
		name += "." + protocol.ID() + ".tmp"
		defer root.Remove(name)
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil || before == nil {
		return err
	}
	_, current, err := c.read()
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("startup configuration changed; its state was preserved")
	}
	return root.Rename(name, "startup.json")
}

func (c *Controller) preserve(entry Entry) error {
	if !entry.Present {
		return nil
	}
	if len(entry.Raw) > 1<<20 {
		return errors.New("existing startup entry exceeds the preservation limit")
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll("startup-history", 0700); err != nil {
		return err
	}
	file, err := root.OpenFile(filepath.Join("startup-history", protocol.ID()+".entry"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(entry.Raw)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func (c *Controller) Status(ctx context.Context, binding Binding) (Info, error) {
	record, _, err := c.read()
	if errors.Is(err, os.ErrNotExist) {
		return Info{Supported: true, Scope: c.backend.Scope(), Name: Name(c.root)}, nil
	}
	if err != nil {
		return Info{}, err
	}
	entry, err := c.backend.Inspect(ctx, record.Definition)
	if err != nil {
		return Info{}, err
	}
	if entry.Present && entry.Owner != record.Definition.Owner {
		return Info{}, errors.New("a different startup entry occupies this name; it was preserved")
	}
	return Info{Supported: true, Registered: entry.Present, Enabled: record.Enabled && entry.Present && entry.Enabled, RequestedEnabled: record.Enabled, EntryMatches: entry.Matches, MatchesProfile: record.Binding == binding, Running: entry.Running, Pending: record.Pending, Scope: record.Definition.Scope, Name: record.Definition.Name}, nil
}

// Apply records intent before OS changes. A missing response can be observed by
// Status; retrying the same intent never needs another ownership token.
func (c *Controller) Apply(ctx context.Context, enabled bool, program string, binding Binding) (Info, error) {
	record, before, err := c.read()
	if errors.Is(err, os.ErrNotExist) {
		record = Record{Format: 1, Definition: c.definition(program, protocol.ID())}
		before = nil
	} else if err != nil {
		return Info{}, err
	}
	if enabled && (!c.ownsProgram(program) || binding.DeviceID == "" || len(binding.ProfileSHA256) != 64 || strings.Trim(binding.ProfileSHA256, "0123456789abcdef") != "") {
		return Info{}, errors.New("startup requires a staged program and current device profile")
	}
	entry, err := c.backend.Inspect(ctx, record.Definition)
	if err != nil {
		return Info{}, err
	}
	if entry.Present && entry.Owner != record.Definition.Owner {
		return Info{}, errors.New("a different startup entry occupies this name; it was preserved")
	}
	if record.Enabled == enabled && ((!enabled && !entry.Present) || (enabled && record.Binding == binding && record.Definition.Program == program && entry.Present && entry.Enabled && entry.Matches)) {
		if before != nil && record.Pending {
			record.Pending = false
			if err = c.save(before, record); err != nil {
				return Info{}, err
			}
		}
		return c.Status(ctx, binding)
	}
	if err = c.preserve(entry); err != nil {
		return Info{}, err
	}
	if enabled {
		record.Definition = c.definition(program, record.Definition.Owner)
		record.Binding = binding
	}
	record.Enabled, record.Pending, record.UpdatedAt = enabled, true, time.Now().UTC().Format(time.RFC3339Nano)
	if err = c.save(before, record); err != nil {
		return Info{}, err
	}
	if enabled {
		err = c.backend.Install(ctx, record.Definition)
	} else if entry.Present {
		err = c.backend.Remove(ctx, record.Definition)
	}
	if err != nil {
		return Info{}, err
	}
	entry, err = c.backend.Inspect(ctx, record.Definition)
	if err != nil {
		return Info{}, err
	}
	if (enabled && (!entry.Present || !entry.Enabled || !entry.Matches || entry.Owner != record.Definition.Owner)) || (!enabled && entry.Present) {
		return Info{}, errors.New("startup registration result remains unconfirmed; observe it before retrying")
	}
	before = protocol.JSON(record)
	record.Pending = false
	if err = c.save(before, record); err != nil {
		return Info{}, err
	}
	return c.Status(ctx, binding)
}

func (c *Controller) Start(ctx context.Context, binding Binding) error {
	record, before, err := c.read()
	if err != nil {
		return err
	}
	if !record.Enabled || record.Binding != binding {
		return errors.New("review startup settings after the device profile changes")
	}
	entry, err := c.backend.Inspect(ctx, record.Definition)
	if err != nil {
		return err
	}
	if !entry.Present || entry.Owner != record.Definition.Owner || !entry.Enabled || !entry.Matches {
		return errors.New("startup entry is missing or belongs to another installation")
	}
	if record.PausedSession != "" {
		record.PausedSession = ""
		if err = c.save(before, record); err != nil {
			return err
		}
	}
	return c.backend.Start(ctx, record.Definition)
}

// PauseRecovery preserves an explicit stop for the current Windows login.
// A fresh login can still honor the separately configured startup preference.
func (c *Controller) PauseRecovery() error {
	backend, supported := c.backend.(recoveryBackend)
	if !supported {
		return nil
	}
	record, before, err := c.read()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !record.Enabled {
		return err
	}
	session, err := backend.recoverySession()
	if err != nil {
		return err
	}
	if session == "" {
		return errors.New("startup recovery could not identify the current login")
	}
	record.PausedSession = session
	return c.save(before, record)
}

// Authorized is used by the foreground OS entry under the installation lease.
// It does not contact the OS manager or implicitly adopt a changed profile.
func (c *Controller) Authorized(owner, program string, binding Binding) (bool, error) {
	record, _, err := c.read()
	if err != nil {
		return false, err
	}
	if !record.Enabled || record.Definition.Owner != owner || record.Definition.Program != program || record.Binding != binding {
		return false, nil
	}
	if record.PausedSession == "" {
		return true, nil
	}
	backend, supported := c.backend.(recoveryBackend)
	if !supported {
		return false, errors.New("startup recovery cannot verify the paused login")
	}
	session, err := backend.recoverySession()
	return err == nil && session != "" && session != record.PausedSession, err
}
