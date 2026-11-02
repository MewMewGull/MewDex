// Package executionscope builds local, content-bound execution permissions.
// It accepts trusted executor paths, never input-client supplied config tables.
package executionscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"mewdex/internal/workspace"
)

type Spec struct {
	DeviceID      string
	ProjectID     string
	EngineVersion string
	Workspace     string
	ReadOnly      []string
	Private       []string
	AccessMode    string
}

type Profile struct {
	id         string
	workspace  string
	filesystem map[string]string
	network    bool
}

func literalPath(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "*?[]\x00\r\n") {
		return "", errors.New("execution permissions require an absolute literal path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", errors.New("execution scope only supports directories and regular files")
	}
	return filepath.Clean(resolved), nil
}

func within(root, target string) bool {
	// Exclusions are conservative on desktop filesystems. Case folding can
	// reject an ambiguous grant, but it must never manufacture an extra grant.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root, target = strings.ToLower(root), strings.ToLower(target)
	}
	relative, err := filepath.Rel(root, target)
	return err == nil && filepath.IsLocal(relative)
}

// PrivateProfilePath also protects adjacent device credentials when a CLI
// profile directory sits inside the execution project. Packaged installations
// normally keep that directory outside every user project.
func PrivateProfilePath(project, profileFile string) (string, error) {
	root, err := literalPath(project)
	if err != nil {
		return "", err
	}
	file, err := literalPath(profileFile)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(file)
	if within(root, directory) {
		return directory, nil
	}
	return file, nil
}

// PublicInputParent selects an existing parent outside private paths. It does
// not create or grant access to anything; Build validates the resulting view.
func PublicInputParent(preferred string, private []string) (string, error) {
	parent, err := workspace.CanonicalDir(preferred)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt <= len(private); attempt++ {
		changed := false
		for _, raw := range private {
			path, err := literalPath(raw)
			if err != nil {
				return "", err
			}
			if within(path, parent) {
				parent = filepath.Dir(path)
				if filepath.Dir(parent) == parent {
					return "", errors.New("no owned input-view parent is available outside private roots")
				}
				changed = true
			}
		}
		if !changed {
			return parent, nil
		}
	}
	return "", errors.New("cannot select a public input-view parent")
}

func Build(spec Spec) (Profile, error) {
	var profile Profile
	if spec.DeviceID == "" || spec.ProjectID == "" || spec.EngineVersion == "" {
		return profile, errors.New("execution scope requires device, project and engine identities")
	}
	root, err := literalPath(spec.Workspace)
	if err != nil {
		return profile, err
	}
	if root, err = workspace.CanonicalDir(root); err != nil {
		return profile, err
	}
	if filepath.Dir(root) == root {
		return profile, errors.New("a filesystem root cannot be an execution project")
	}
	filesystem := map[string]string{":minimal": "read", root: "write"}
	if spec.AccessMode == "read-only" {
		filesystem[root] = "read"
	}
	if spec.AccessMode != "" && spec.AccessMode != "workspace" && spec.AccessMode != "read-only" && spec.AccessMode != "workspace-network" {
		return profile, errors.New("unsupported execution access mode")
	}
	network := spec.AccessMode == "workspace-network"
	privateSet := map[string]bool{}
	for _, raw := range spec.Private {
		path, err := literalPath(raw)
		if err != nil {
			return profile, fmt.Errorf("private execution path: %w", err)
		}
		if within(path, root) {
			return profile, errors.New("execution project is inside private node data; select a separate project directory")
		}
		privateSet[path] = true
	}
	privateSources := make([]string, 0, len(privateSet))
	for path := range privateSet {
		privateSources = append(privateSources, path)
	}
	sort.Strings(privateSources)
	// Bubblewrap cannot create a second denied mount below an already denied
	// read-only mount. Collapse only literal descendants, never case-folded
	// lookalikes that may refer to distinct files on a case-sensitive volume.
	for _, path := range privateSources {
		covered := false
		for _, parent := range privateSources {
			if path != parent && strings.HasPrefix(path, parent+string(filepath.Separator)) {
				covered = true
				break
			}
		}
		if !covered {
			filesystem[path] = "deny"
		}
	}
	for _, raw := range spec.ReadOnly {
		path, err := literalPath(raw)
		if err != nil {
			return profile, fmt.Errorf("read-only execution path: %w", err)
		}
		insidePrivate := false
		for denied, access := range filesystem {
			if access == "deny" && within(denied, path) {
				insidePrivate = true
			}
		}
		if insidePrivate || within(path, root) {
			return profile, errors.New("read-only inputs and public runtimes must be outside private roots and cannot contain the writable project")
		}
		filesystem[path] = "read"
	}
	// encoding/json orders string map keys. Equivalent path sets produce one
	// immutable identity; changing any grant cannot reuse a broader old profile.
	identity, err := json.Marshal(struct {
		Format                             int `json:"format"`
		Device, Project, Engine, Workspace string
		Filesystem                         map[string]string
		PrivateSources                     []string
		ExecutionSettings                  map[string]any
		Network                            bool
	}{3, spec.DeviceID, spec.ProjectID, spec.EngineVersion, root, filesystem, privateSources, executionSettings(), network})
	if err != nil {
		return profile, err
	}
	digest := sha256.Sum256(identity)
	profile.id = "mewdex-scope-" + hex.EncodeToString(digest[:])
	profile.workspace, profile.filesystem = root, filesystem
	profile.network = network
	return profile, nil
}

func (p Profile) ID() string        { return p.id }
func (p Profile) Workspace() string { return p.workspace }

func (p Profile) PrivatePaths() []string {
	paths := []string{}
	for path, access := range p.filesystem {
		if access == "deny" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (p Profile) Definition() map[string]any {
	filesystem := map[string]string{}
	for path, access := range p.filesystem {
		filesystem[path] = access
	}
	return map[string]any{"filesystem": filesystem, "network": map[string]bool{"enabled": p.network}}
}

// ThreadConfig is supplied to thread/start, resume and fork. Selecting this
// profile alongside a legacy sandbox value would be an invalid engine request.
func (p Profile) ThreadConfig() map[string]any {
	config := executionSettings()
	config["permissions"] = map[string]any{p.id: p.Definition()}
	return config
}

func executionSettings() map[string]any {
	settings := map[string]any{
		"shell_environment_policy": map[string]any{
			"inherit": "core", "ignore_default_excludes": false,
			"exclude": []string{"*KEY*", "*SECRET*", "*TOKEN*", "*PASSWORD*", "MEWDEX_*"},
			// The final allowlist also filters values restored by a lower layer's
			// `set` table. An empty override alone does not clear that table.
			"include_only": []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMP", "TEMP", "TMPDIR", "SystemRoot", "SystemDrive", "COMSPEC", "PATHEXT", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "LOCALAPPDATA", "APPDATA", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "LANG", "LC_*", "TZ", "TERM"},
			"set":          map[string]string{},
		},
		"allow_login_shell": false,
		"features":          map[string]bool{"shell_snapshot": false, "request_permissions_tool": true},
	}
	if runtime.GOOS == "windows" {
		settings["windows"] = map[string]string{"sandbox": "mxc"}
	}
	return settings
}

// VerifyThread checks the effective settings returned by the pinned engine.
// The legacy sandbox field is deliberately not treated as read-scope evidence.
func (p Profile) VerifyThread(raw json.RawMessage) error {
	var response struct {
		Cwd      string          `json:"cwd"`
		Approval json.RawMessage `json:"approvalPolicy"`
		Reviewer string          `json:"approvalsReviewer"`
		Active   *struct {
			ID      string  `json:"id"`
			Extends *string `json:"extends"`
		} `json:"activePermissionProfile"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Active == nil || response.Active.ID != p.id || response.Active.Extends != nil || response.Cwd != p.workspace || response.Reviewer != "user" || string(response.Approval) != `"on-request"` {
		return errors.New("engine did not confirm the bound execution profile, project and approval settings")
	}
	return nil
}
