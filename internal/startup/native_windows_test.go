//go:build windows

package startup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSchedulerDefaultsAndAccountNormalization(t *testing.T) {
	platform, err := Native()
	if err != nil {
		t.Fatal(err)
	}
	b := platform.(*nativeBackend)
	root := filepath.Join(t.TempDir(), "设备 服务")
	c, err := New(root, b)
	if err != nil {
		t.Fatal(err)
	}
	d := c.definition(filepath.Join(root, "runtime", "mewdex.exe"), strings.Repeat("a", 32))
	raw, err := b.render(d)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(raw), "<RunLevel>LeastPrivilege</RunLevel>", "")
	normalized = strings.ReplaceAll(normalized, "<Enabled>true</Enabled>", "")
	normalized = strings.Replace(normalized, "<UserId>"+b.sid+"</UserId>", "<UserId>"+b.account+"</UserId>", 1)
	if !b.matches(`<?xml version="1.0" encoding="UTF-16"?>`+normalized, d) {
		t.Fatal("scheduler's default omission or account name was treated as another definition")
	}
	wrong := strings.Replace(normalized, "<LogonType>InteractiveToken</LogonType>", "<LogonType>Password</LogonType>", 1)
	if b.matches(wrong, d) {
		t.Fatal("password-based registration matched interactive-only startup")
	}
	wrong = strings.Replace(string(raw), "<RunLevel>LeastPrivilege</RunLevel>", "<RunLevel>HighestAvailable</RunLevel>", 1)
	if b.matches(wrong, d) {
		t.Fatal("elevated registration matched least-privilege startup")
	}
}
