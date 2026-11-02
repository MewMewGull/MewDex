package hubproxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sshConfigFixture(t *testing.T, config string) string {
	t.Helper()
	home := t.TempDir()
	directory := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"config": config, "id_ed25519": "PRIVATE-KEY-MARKER-NOT-FOR-UI", "key with spaces": "PRIVATE-KEY-MARKER-NOT-FOR-UI", "known_hosts": "KNOWN-HOST-KEY-MARKER-NOT-FOR-UI"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestSSHConfigResolvesConcreteAliasesDefaultsAndPathsWithoutKeyContents(t *testing.T) {
	home := sshConfigFixture(t, "\ufeff# local config\r\nHost alpha beta\r\n HostName server.example\r\n User deploy\r\n Port=2222\r\n IdentityFile \"%d/.ssh/key with spaces\" # first identity\r\nHost beta\r\n User ignored-later-value\r\nHost * !excluded\r\n Port 22\r\n User fallback\r\n IdentityFile ~/.ssh/id_ed25519\r\n")
	info, err := ReadSSHConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Found || info.ConfigPath != filepath.Join(home, ".ssh", "config") || len(info.Profiles) != 2 {
		t.Fatalf("concrete hosts were not enumerated: %#v", info)
	}
	for _, profile := range info.Profiles {
		if !profile.Importable || profile.Host != "server.example" || profile.Port != 2222 || profile.User != "deploy" || profile.IdentityFile != filepath.Join(home, ".ssh", "key with spaces") || profile.KnownHostsFile != filepath.Join(home, ".ssh", "known_hosts") {
			t.Fatalf("SSH precedence, quotes or expansion changed: %#v", profile)
		}
	}
	raw, _ := json.Marshal(info)
	if strings.Contains(string(raw), "PRIVATE-KEY-MARKER") || strings.Contains(string(raw), "KNOWN-HOST-KEY-MARKER") {
		t.Fatal("SSH file contents leaked through discovery metadata")
	}
	// Deliberately non-PEM key bytes above are still discoverable: import only
	// checks file references. Authentication is validated by the existing save.
	key, _ := os.ReadFile(filepath.Join(home, ".ssh", "key with spaces"))
	if string(key) != "PRIVATE-KEY-MARKER-NOT-FOR-UI" {
		t.Fatal("reading config modified an identity file")
	}
}

func TestSSHDiscoveryUsesExistingDefaultFilesAndReportsIncompleteProfiles(t *testing.T) {
	home := sshConfigFixture(t, "Host available\n HostName host.example\n User deploy\nHost missing\n IdentityFile ~/.ssh/absent\n UserKnownHostsFile ~/.ssh/unknown\nHost public-key\n IdentityFile ~/.ssh/wrong.pub\nHost jump\n ProxyJump gateway\nHost command\n ProxyCommand secret-inline-value\n")
	_ = os.WriteFile(filepath.Join(home, ".ssh", "wrong.pub"), []byte("public"), 0600)
	info, err := ReadSSHConfig(home)
	if err != nil || len(info.Profiles) != 5 {
		t.Fatalf("discovery failed: %v", err)
	}
	if !info.Profiles[0].Importable || info.Profiles[0].Port != 22 || info.Profiles[0].IdentityFile != filepath.Join(home, ".ssh", "id_ed25519") {
		t.Fatal("default identity or SSH port was not resolved")
	}
	for _, profile := range info.Profiles[1:] {
		if profile.Importable || len(profile.Warnings) == 0 {
			t.Fatalf("unsupported or incomplete profile was silently accepted: %#v", profile)
		}
	}
	raw, _ := json.Marshal(info)
	if strings.Contains(string(raw), "secret-inline-value") {
		t.Fatal("a ProxyCommand value was returned to the UI")
	}
}

func TestSSHConfigNeverEvaluatesIncludeMatchOrCommands(t *testing.T) {
	for _, directive := range []string{"Include ~/.ssh/id_ed25519", "include=*.conf", "Match exec \"create-a-marker\""} {
		t.Run(strings.Fields(directive)[0], func(t *testing.T) {
			home := sshConfigFixture(t, directive+"\n HostName private-inline-marker\n")
			info, err := ReadSSHConfig(home)
			if err == nil || len(info.Profiles) != 0 || strings.Contains(err.Error(), "private-inline-marker") || strings.Contains(err.Error(), "create-a-marker") {
				t.Fatalf("conditional configuration was evaluated or exposed: %#v / %v", info, err)
			}
		})
	}
}

func TestSSHDiscoveryMissingOrInvalidConfigLeavesManualSetupAvailable(t *testing.T) {
	info, err := ReadSSHConfig(t.TempDir())
	if err != nil || info.Found || len(info.Profiles) != 0 || info.Message == "" {
		t.Fatal("missing SSH config was not handled as a normal manual setup")
	}
	home := sshConfigFixture(t, "Host *\n Port 22\n")
	info, err = ReadSSHConfig(home)
	if err != nil || !info.Found || len(info.Profiles) != 0 || info.Message == "" {
		t.Fatal("a wildcard-only config was presented as a concrete destination")
	}
	home = sshConfigFixture(t, "Host target\nIdentityFile \"not-closed-secret-value\n")
	_, err = ReadSSHConfig(home)
	if err == nil || strings.Contains(err.Error(), "not-closed-secret-value") {
		t.Fatal("raw malformed SSH content escaped through an error")
	}
}
