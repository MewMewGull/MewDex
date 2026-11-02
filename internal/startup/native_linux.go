//go:build linux

package startup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type nativeBackend struct {
	directory string
	command   string
	system    bool
}

func Native() (Backend, error) {
	b := &nativeBackend{directory: "/etc/systemd/system", system: os.Geteuid() == 0}
	if !b.system {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		b.directory = filepath.Join(home, ".config", "systemd", "user")
	}
	for _, path := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			b.command = path
			return b, nil
		}
	}
	return nil, errors.New("systemd service management is unavailable")
}

func (b *nativeBackend) Scope() string {
	if b.system {
		return "system-boot"
	}
	return "user-login"
}

func quoteSystemd(value string) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$").Replace(value)
	return "\"" + value + "\""
}
func quoteSystemdValue(value string) string {
	// Path-valued directives are literal values, not ExecStart argument lists.
	return strings.ReplaceAll(value, "%", "%%")
}

func (b *nativeBackend) render(d Definition) []byte {
	command := []string{quoteSystemd(d.Program)}
	for _, arg := range d.Arguments {
		command = append(command, quoteSystemd(arg))
	}
	target := "default.target"
	if b.system {
		target = "multi-user.target"
	}
	return []byte(fmt.Sprintf("# MewDex:%s\n[Unit]\nDescription=MewDex device service\nAfter=network.target\n\n[Service]\nType=exec\nExecStart=%s\nWorkingDirectory=%s\nRestart=on-failure\nRestartSec=5s\nKillMode=control-group\nTimeoutStopSec=30s\nUMask=0077\nStandardOutput=append:%s\nStandardError=inherit\n\n[Install]\nWantedBy=%s\n", d.Owner, strings.Join(command, " "), quoteSystemdValue(d.Root), quoteSystemdValue(filepath.Join(d.Root, "service.log")), target))
}

func (b *nativeBackend) path(d Definition) string {
	return filepath.Join(b.directory, d.Name+".service")
}
func (b *nativeBackend) ctl(ctx context.Context, arguments ...string) ([]byte, error) {
	if !b.system {
		arguments = append([]string{"--user"}, arguments...)
	}
	return run(ctx, b.command, arguments, nil)
}
func systemdOwner(raw []byte) string {
	line, _, _ := strings.Cut(string(raw), "\n")
	if !strings.HasPrefix(line, "# MewDex:") {
		return ""
	}
	return strings.TrimPrefix(line, "# MewDex:")
}

func (b *nativeBackend) Inspect(ctx context.Context, d Definition) (Entry, error) {
	raw, err := readEntry(b.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, err
	}
	status, enabledErr := b.ctl(ctx, "is-enabled", d.Name+".service")
	state := strings.TrimSpace(string(status))
	if enabledErr != nil && state != "disabled" && state != "static" && state != "indirect" && state != "not-found" {
		return Entry{}, errors.New("systemd startup status is unavailable")
	}
	active, _ := b.ctl(ctx, "is-active", d.Name+".service")
	return Entry{Present: true, Owner: systemdOwner(raw), Enabled: state == "enabled" || state == "enabled-runtime", Running: strings.TrimSpace(string(active)) == "active", Matches: bytes.Equal(raw, b.render(d)), Raw: raw}, nil
}

func (b *nativeBackend) owned(d Definition) ([]byte, error) {
	raw, err := readEntry(b.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if systemdOwner(raw) != d.Owner {
		return nil, errors.New("systemd startup entry belongs to another installation")
	}
	return raw, nil
}

func (b *nativeBackend) Install(ctx context.Context, d Definition) error {
	if strings.TrimSpace(d.Root) != d.Root || strings.HasSuffix(d.Root, "\\") {
		return errors.New("systemd startup cannot represent an installation path ending with whitespace or a backslash")
	}
	before, err := b.owned(d)
	if err != nil {
		return err
	}
	if err = writeEntry(b.path(d), before, b.render(d)); err != nil {
		return err
	}
	if _, err = b.ctl(ctx, "daemon-reload"); err != nil {
		return errors.New("systemd could not load the startup definition")
	}
	if _, err = b.ctl(ctx, "enable", d.Name+".service"); err != nil {
		return errors.New("systemd could not enable this startup entry")
	}
	return nil
}
func (b *nativeBackend) Remove(ctx context.Context, d Definition) error {
	before, err := b.owned(d)
	if err != nil || before == nil {
		return err
	}
	if _, err = b.ctl(ctx, "disable", d.Name+".service"); err != nil {
		return errors.New("systemd could not disable this startup entry")
	}
	if err = removeEntry(b.path(d), before); err != nil {
		return err
	}
	_, err = b.ctl(ctx, "daemon-reload")
	return err
}
func (b *nativeBackend) Start(ctx context.Context, d Definition) error {
	if _, err := b.owned(d); err != nil {
		return err
	}
	_, err := b.ctl(ctx, "start", d.Name+".service")
	if err != nil {
		return errors.New("systemd could not start this MewDex entry; inspect its retained startup definition and service log")
	}
	return nil
}
func (b *nativeBackend) Stop(ctx context.Context, d Definition) error {
	if _, err := b.owned(d); err != nil {
		return err
	}
	state, _ := b.ctl(ctx, "show", d.Name+".service", "--property=LoadState", "--value")
	if strings.TrimSpace(string(state)) == "not-found" {
		return nil
	}
	_, err := b.ctl(ctx, "stop", d.Name+".service")
	return err
}
