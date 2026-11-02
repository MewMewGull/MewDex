package backhaul

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureConfig(t *testing.T) (string, Config) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "owned fixture with spaces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	program := []byte("synthetic SSH executable; never launched by this fixture")
	hash := sha256.Sum256(program)
	c := Config{ID: "test-backhaul", SSHPath: filepath.Join(root, "ssh fixture"), SSHSHA256: hex.EncodeToString(hash[:]), Host: "fixture.invalid", User: "user", Port: 22, IdentityFile: filepath.Join(root, "existing key"), KnownHostsFile: filepath.Join(root, "known hosts"), RemoteBind: "127.0.0.1", RemotePort: 47834, LocalPort: 47830, PublicURL: "https://fixture.invalid"}
	for path, content := range map[string][]byte{c.SSHPath: program, c.IdentityFile: []byte("synthetic reference only"), c.KnownHostsFile: []byte("synthetic known hosts")} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root, c
}

func writeProfile(t *testing.T, root string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "backhaul.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBackhaulProfileDoesNotAdoptNewAccessOrAnotherSSHExecutable(t *testing.T) {
	root, base := fixtureConfig(t)
	writeProfile(t, root, base)
	if c, binding, err := Read(root); err != nil || binding.DeviceID != base.ID || c.VerifySSH() != nil {
		t.Fatal(binding, err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.RemoteBind = "0.0.0.0" },
		func(c *Config) { c.RemoteBind = "203.0.113.1" },
		func(c *Config) { c.Host = "-oProxyCommand=arbitrary" },
		func(c *Config) { c.User = "user with flags" },
		func(c *Config) { c.PublicURL = "http://example.com" },
		func(c *Config) { c.PublicURL = "https://user:password@example.com" },
		func(c *Config) { c.PublicURL = "https://example.com?token=secret" },
		func(c *Config) { c.IdentityFile = "relative-key" },
		func(c *Config) { c.LocalPort = 0 },
	} {
		candidate := base
		mutate(&candidate)
		writeProfile(t, root, candidate)
		if _, _, err := Read(root); err == nil {
			t.Fatal("accepted an unreviewable endpoint or credential reference", candidate)
		}
	}
	var unknown map[string]any
	raw, _ := json.Marshal(base)
	_ = json.Unmarshal(raw, &unknown)
	unknown["privateKey"] = "not a supported profile field"
	writeProfile(t, root, unknown)
	if _, _, err := Read(root); err == nil {
		t.Fatal("accepted an embedded secret or unknown option")
	}
	if err := os.WriteFile(base.SSHPath, []byte("different executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := base.VerifySSH(); err == nil {
		t.Fatal("SSH executable update bypassed its reviewed hash")
	}
}

func TestSSHInvocationUsesOnlyTheReviewedForwardAndExistingCredentialReferences(t *testing.T) {
	_, c := fixtureConfig(t)
	args := c.sshArguments()
	options := map[string]string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" {
			i++
			pair := strings.SplitN(args[i], "=", 2)
			options[pair[0]] = pair[1]
		}
	}
	for key, want := range map[string]string{"BatchMode": "yes", "StrictHostKeyChecking": "yes", "UpdateHostKeys": "no", "ExitOnForwardFailure": "yes", "ForwardAgent": "no", "ForwardX11": "no", "PermitLocalCommand": "no", "ControlMaster": "no"} {
		if options[key] != want {
			t.Fatal("unexpected authentication or connection sharing", key, options[key])
		}
	}
	if !strings.HasPrefix(options["UserKnownHostsFile"], `"`) || !strings.Contains(options["UserKnownHostsFile"], "known hosts") {
		t.Fatal("credential reference with spaces was split by SSH option parsing")
	}
	if args[0] != "-F" || args[1] != "none" || args[len(args)-1] != c.Host || args[len(args)-2] != "127.0.0.1:47834:127.0.0.1:47830" {
		t.Fatal("inherited configuration or a different forwarding target was admitted", args)
	}
}
