//go:build windows

package startup

import (
	"encoding/xml"
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
	normalized = strings.Replace(normalized, "2020-01-01T00:00:00Z", "2020-01-01T08:00:00+08:00", 1)
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
	var legacy taskXML
	if err := xml.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Recovery.Repetition.Interval = ""
	withoutRecovery, err := xml.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if b.matches(string(withoutRecovery), d) {
		t.Fatal("a registration without periodic crash recovery was treated as current")
	}
	legacy.Recovery.Repetition.Interval = "PT1M"
	legacy.Settings.MultipleInstances = "StopExisting"
	unsafeRecovery, err := xml.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if b.matches(string(unsafeRecovery), d) {
		t.Fatal("a timer that interrupts a healthy running service was accepted")
	}
}

func TestNativeRecoverySessionIsStableWithinTheLogin(t *testing.T) {
	backend := &nativeBackend{}
	first, err := backend.recoverySession()
	if err != nil || len(first) != 64 {
		t.Fatal("current login identity is unavailable", err)
	}
	second, err := backend.recoverySession()
	if err != nil || first != second {
		t.Fatal("current login identity changed between observations", err)
	}
}
