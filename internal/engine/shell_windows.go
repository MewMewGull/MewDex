//go:build windows

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Use the system shell that is covered by the Windows platform read roots.
// A hosting editor's portable shell must not cause a broader directory grant.
func nativeEngineShell() (string, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	shell := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	info, err := os.Stat(shell)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("Windows system PowerShell is unavailable")
	}
	return shell, nil
}

// Windows shell discovery prefers any pwsh on PATH over system PowerShell.
// Portable editor runtimes are outside the platform read roots, so omit only
// those PowerShell runtime entries from this engine's PATH. Other tool paths
// remain available; no filesystem grants are added for a hosting editor.
func nativeEngineSearchPath(original string) string {
	public := []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432")}
	paths := []string{}
	for _, entry := range filepath.SplitList(original) {
		info, err := os.Stat(filepath.Join(strings.Trim(entry, `"`), "pwsh.exe"))
		if err != nil || !info.Mode().IsRegular() {
			paths = append(paths, entry)
			continue
		}
		allowed := false
		for _, root := range public {
			if root == "" {
				continue
			}
			relative, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(strings.Trim(entry, `"`))))
			if err == nil && filepath.IsLocal(relative) {
				allowed = true
				break
			}
		}
		if allowed {
			paths = append(paths, entry)
		}
	}
	return strings.Join(paths, string(filepath.ListSeparator))
}
