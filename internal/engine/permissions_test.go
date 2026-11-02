package engine

import (
	"encoding/json"
	"testing"
)

func TestPermissionRequestsAcceptOnlyKnownLiteralAdditions(t *testing.T) {
	for _, test := range []struct {
		permissions string
		valid       bool
	}{
		{`{"fileSystem":{"entries":[{"path":{"type":"path","path":"/project/input"},"access":"read"}],"globScanMaxDepth":null},"network":{"enabled":true}}`, true},
		{`{"fileSystem":{"read":["/project/input"],"write":["/project/output"]}}`, true},
		{`{"fileSystem":{"entries":[{"path":{"type":"special","value":{"kind":"root"}},"access":"read"}]}}`, false},
		{`{"fileSystem":{"entries":[{"path":{"type":"glob_pattern","pattern":"/**"},"access":"read"}]}}`, false},
		{`{"fileSystem":{"entries":[{"path":{"type":"path","path":"/project/input"},"access":"deny"}]}}`, false},
		{`{"fileSystem":{"globScanMaxDepth":5}}`, false},
		{`{"network":{"enabled":true,"unknown":true}}`, false},
		{`{"unknown":true}`, false},
		{`null`, false},
	} {
		n := Notification{Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"permissions":` + test.permissions + `}`)}
		grant, err := RequestedPermissions(n)
		if (err == nil) != test.valid {
			t.Fatalf("%s: %+v %v", test.permissions, grant, err)
		}
	}
}
