package executionscope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func profileFixture(t *testing.T) Spec {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project, private := filepath.Join(root, "项目 workspace"), filepath.Join(root, "项目 workspace", "node-data")
	input := filepath.Join(root, "read-only-inputs", "session-a")
	runtime := filepath.Join(root, "public-runtime")
	for _, path := range []string{input, runtime, private} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return Spec{DeviceID: "device-a", ProjectID: "project-a", EngineVersion: "pinned", Workspace: project, Private: []string{private}, ReadOnly: []string{runtime, input}}
}

func TestScopeBindsCanonicalGrantsAndIdentity(t *testing.T) {
	spec := profileFixture(t)
	profile, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.ReadOnly[0], spec.ReadOnly[1] = spec.ReadOnly[1], spec.ReadOnly[0]
	same, err := Build(spec)
	if err != nil || same.ID() != profile.ID() {
		t.Fatal("equivalent grant order changed identity", err)
	}
	for _, field := range []string{"device", "project", "engine", "grant"} {
		changed := spec
		changed.ReadOnly = append([]string(nil), spec.ReadOnly...)
		switch field {
		case "device":
			changed.DeviceID += "-b"
		case "project":
			changed.ProjectID += "-b"
		case "engine":
			changed.EngineVersion += "-b"
		case "grant":
			changed.ReadOnly = changed.ReadOnly[:1]
		}
		other, err := Build(changed)
		if err != nil || other.ID() == profile.ID() {
			t.Fatalf("%s change reused the old profile: %v", field, err)
		}
	}
	definition := profile.Definition()
	filesystem := definition["filesystem"].(map[string]string)
	if filesystem[spec.Workspace] != "write" || filesystem[spec.Private[0]] != "deny" || filesystem[spec.ReadOnly[0]] != "read" || definition["network"].(map[string]bool)["enabled"] {
		t.Fatal("lost an execution boundary")
	}
	filesystem[spec.Private[0]] = "write"
	if profile.Definition()["filesystem"].(map[string]string)[spec.Private[0]] != "deny" {
		t.Fatal("caller mutated a retained scope")
	}
}

func TestProjectDirectoryGroupSharesOnlyItsLocalRoots(t *testing.T) {
	spec := profileFixture(t)
	second := t.TempDir()
	spec.Workspaces = []string{second}
	profile, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := literalPath(second)
	if err != nil || profile.filesystem[canonical] != "write" {
		t.Fatal("project group root was omitted", err)
	}
	spec.AccessMode = "read-only"
	profile, err = Build(spec)
	if err != nil || profile.filesystem[canonical] != "read" || profile.filesystem[profile.Workspace()] != "read" {
		t.Fatal("read-only policy did not cover the whole project group", err)
	}
	spec.Workspaces = []string{spec.Private[0]}
	if _, err = Build(spec); err == nil {
		t.Fatal("private node data became a project group root")
	}
}

func TestSelectedAccessModesChangeEffectiveGrants(t *testing.T) {
	spec := profileFixture(t)
	ids := map[string]bool{}
	for _, mode := range []string{"", "read-only", "workspace-network"} {
		spec.AccessMode = mode
		profile, err := Build(spec)
		if err != nil {
			t.Fatal(err)
		}
		definition := profile.Definition()
		access := definition["filesystem"].(map[string]string)[spec.Workspace]
		if (mode == "read-only" && access != "read") || (mode != "read-only" && access != "write") {
			t.Fatal("incorrect project access", mode, access)
		}
		if definition["network"].(map[string]bool)["enabled"] != (mode == "workspace-network") {
			t.Fatal("incorrect network access", mode)
		}
		if definition["filesystem"].(map[string]string)[spec.Private[0]] != "deny" {
			t.Fatal("access selection exposed node data")
		}
		if ids[profile.ID()] {
			t.Fatal("different effective permissions reused a profile")
		}
		ids[profile.ID()] = true
	}
}

func TestScopeRejectsAmbiguousAndConflictingPaths(t *testing.T) {
	for _, mutation := range []string{"relative", "glob", "missing", "private-project", "private-read", "nested-private-read", "wide-read"} {
		t.Run(mutation, func(t *testing.T) {
			spec := profileFixture(t)
			switch mutation {
			case "relative":
				spec.Workspace = "."
			case "glob":
				spec.Workspace += "*"
			case "missing":
				spec.ReadOnly = append(spec.ReadOnly, filepath.Join(spec.Workspace, "missing"))
			case "private-project":
				spec.Private = []string{filepath.Dir(spec.Workspace)}
			case "private-read":
				spec.ReadOnly = append(spec.ReadOnly, spec.Private[0])
			case "nested-private-read":
				path := filepath.Join(spec.Private[0], "input")
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				spec.ReadOnly = append(spec.ReadOnly, path)
			case "wide-read":
				spec.ReadOnly = append(spec.ReadOnly, filepath.Dir(spec.Workspace))
			}
			if _, err := Build(spec); err == nil {
				t.Fatal("accepted a conflicting execution grant")
			}
		})
	}
}

func TestEngineResponseMustConfirmTheBoundScope(t *testing.T) {
	profile, err := Build(profileFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"none", "legacy-only", "profile", "cwd", "approval", "reviewer", "parent"} {
		t.Run(mutation, func(t *testing.T) {
			active := map[string]any{"id": profile.ID(), "extends": nil}
			response := map[string]any{"activePermissionProfile": active, "cwd": profile.Workspace(), "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": map[string]string{"type": "workspaceWrite"}}
			switch mutation {
			case "legacy-only":
				delete(response, "activePermissionProfile")
			case "profile":
				active["id"] = ":workspace"
			case "cwd":
				response["cwd"] = filepath.Dir(profile.Workspace())
			case "approval":
				response["approvalPolicy"] = "never"
			case "reviewer":
				response["approvalsReviewer"] = "auto_review"
			case "parent":
				active["extends"] = ":workspace"
			}
			raw, _ := json.Marshal(response)
			if err := profile.VerifyThread(raw); (err == nil) != (mutation == "none") {
				t.Fatalf("%s: %v", mutation, err)
			}
		})
	}
}

func TestProjectLocalProfileProtectsAdjacentCredentialsAndMovesViewsOutside(t *testing.T) {
	spec := profileFixture(t)
	local := filepath.Join(spec.Workspace, ".local")
	if err := os.Mkdir(local, 0700); err != nil {
		t.Fatal(err)
	}
	profileFile := filepath.Join(local, "device.json")
	if err := os.WriteFile(profileFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	protected, err := PrivateProfilePath(spec.Workspace, profileFile)
	if err != nil || protected != local {
		t.Fatal("adjacent credentials were left readable", err)
	}
	parent, err := PublicInputParent(local, []string{local})
	if err != nil || parent != spec.Workspace {
		t.Fatal("view remains under a denied parent", err)
	}
	outside, err := PrivateProfilePath(spec.ReadOnly[1], profileFile)
	if err != nil || outside != profileFile {
		t.Fatal("unrelated project broadened its excluded path", err)
	}
}

func TestNestedPrivateRulesAreCollapsedWithoutLosingTheirBinding(t *testing.T) {
	spec := profileFixture(t)
	parent := spec.Private[0]
	child := filepath.Join(parent, "account-home")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	baseline, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Private = append(spec.Private, child)
	profile, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	filesystem := profile.Definition()["filesystem"].(map[string]string)
	if filesystem[parent] != "deny" || filesystem[child] != "" {
		t.Fatal("redundant child deny would break native Linux mounts")
	}
	if profile.ID() == baseline.ID() {
		t.Fatal("a changed account/data source lost its binding after collapsing effective rules")
	}
}
