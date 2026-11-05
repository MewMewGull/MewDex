//go:build linux

package hostidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClonedMachineIDsUsePhysicalNetworkIdentity(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "eth0")
	if err := os.MkdirAll(filepath.Join(physical, "device"), 0700); err != nil {
		t.Fatal(err)
	}
	address := filepath.Join(physical, "address")
	os.WriteFile(address, []byte("52:54:00:11:22:33\n"), 0600)
	first := linuxIdentity("shared-machine-id", root)
	os.WriteFile(address, []byte("52:54:00:44:55:66\n"), 0600)
	second := linuxIdentity("shared-machine-id", root)
	if first == second {
		t.Fatal("cloned machine-id collapsed different machines")
	}
	os.Mkdir(filepath.Join(root, "docker0"), 0700)
	os.WriteFile(filepath.Join(root, "docker0/address"), []byte("02:00:00:00:00:01\n"), 0600)
	if linuxIdentity("shared-machine-id", root) != second {
		t.Fatal("a virtual network changed the host identity")
	}
	os.WriteFile(filepath.Join(physical, "addr_assign_type"), []byte("3\n"), 0600)
	if linuxIdentity("shared-machine-id", root) != "shared-machine-id" {
		t.Fatal("a randomized address entered the persistent identity")
	}
}
