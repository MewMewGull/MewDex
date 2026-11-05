//go:build linux

package hostidentity

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func nativeID(_ context.Context) (string, error) {
	value, err := os.ReadFile("/etc/machine-id")
	if os.IsNotExist(err) {
		value, err = os.ReadFile("/var/lib/dbus/machine-id")
	}
	if err != nil {
		return "", err
	}
	return linuxIdentity(strings.TrimSpace(string(value)), "/sys/class/net"), nil
}

// Cloned cloud images can share machine-id. A physical NIC distinguishes those
// installations without requiring root-only DMI access. Virtual bridge/veth and
// randomized addresses are excluded so reconnecting does not change identity.
func linuxIdentity(machineID, networkRoot string) string {
	entries, _ := os.ReadDir(networkRoot)
	type adapter struct{ path, address string }
	var adapters []adapter
	for _, entry := range entries {
		root := filepath.Join(networkRoot, entry.Name())
		device, err := filepath.EvalSymlinks(filepath.Join(root, "device"))
		if err != nil {
			continue
		}
		assigned, _ := os.ReadFile(filepath.Join(root, "addr_assign_type"))
		if kind := strings.TrimSpace(string(assigned)); kind != "" && kind != "0" {
			continue
		}
		value, _ := os.ReadFile(filepath.Join(root, "address"))
		address := strings.ToLower(strings.TrimSpace(string(value)))
		mac, err := net.ParseMAC(address)
		if err != nil || len(mac) != 6 || mac[0]&1 != 0 || address == "00:00:00:00:00:00" {
			continue
		}
		adapters = append(adapters, adapter{path: device, address: address})
	}
	if len(adapters) == 0 {
		return machineID
	}
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].path < adapters[j].path })
	return machineID + "\x00" + adapters[0].address
}
