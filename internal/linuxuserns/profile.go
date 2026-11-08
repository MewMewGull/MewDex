// Package linuxuserns prepares a pinned, root-owned Bubblewrap helper for hosts
// that mediate unprivileged user namespaces through AppArmor.
package linuxuserns

import (
	"fmt"
	"path/filepath"
)

const BubblewrapSHA256 = "01fb705f067bd5365b63d8ad2323a61c8d007733ca5e649437e086f3fb9935d8"
const helperRoot = "/usr/libexec/mewdex-sandbox"
const profileRoot = "/etc/apparmor.d"
const parserPath = "/usr/sbin/apparmor_parser"

type Plan struct {
	SourceBwrap        string `json:"sourceBwrap"`
	SHA256             string `json:"sha256"`
	HelperPath         string `json:"helperPath"`
	ProfilePath        string `json:"profilePath"`
	ProfileText        string `json:"profileText"`
	RestrictionEnabled bool   `json:"restrictionEnabled"`
	Administrator      bool   `json:"administrator"`
	HelperInstalled    bool   `json:"helperInstalled"`
	ProfileInstalled   bool   `json:"profileInstalled"`
	ProfileLoaded      bool   `json:"profileLoaded"`
	Configured         bool   `json:"configured"`
}

func HelperPath() string  { return filepath.Join(helperRoot, BubblewrapSHA256, "bwrap") }
func profileName() string { return "mewdex-bwrap-" + BubblewrapSHA256 }
func profilePath() string { return filepath.Join(profileRoot, profileName()) }
func profileText() string {
	return fmt.Sprintf("abi <abi/4.0>,\ninclude <tunables/global>\n\nprofile %s %s flags=(unconfined) {\n  userns,\n}\n", profileName(), HelperPath())
}
