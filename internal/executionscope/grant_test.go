package executionscope

import (
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/protocol"
)

func TestAdditionalGrantsCannotExposePrivateOrModifyReadonlyPaths(t *testing.T) {
	spec := profileFixture(t)
	profile, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	material := filepath.Join(filepath.Dir(spec.Workspace), "other-project")
	if err := os.Mkdir(material, 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		grant protocol.PermissionGrant
		valid bool
	}{
		{"external-read", protocol.PermissionGrant{ReadPaths: []string{material}}, true},
		{"external-write", protocol.PermissionGrant{WritePaths: []string{material}}, true},
		{"network", protocol.PermissionGrant{Network: true}, true},
		{"private", protocol.PermissionGrant{ReadPaths: spec.Private}, false},
		{"private-parent", protocol.PermissionGrant{ReadPaths: []string{spec.Workspace}}, false},
		{"readonly-write", protocol.PermissionGrant{WritePaths: spec.ReadOnly}, false},
		{"root", protocol.PermissionGrant{ReadPaths: []string{filepath.VolumeName(material) + string(filepath.Separator)}}, false},
		{"glob", protocol.PermissionGrant{ReadPaths: []string{material + "*"}}, false},
		{"empty", protocol.PermissionGrant{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := profile.ValidateGrant(test.grant)
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
		})
	}
}
