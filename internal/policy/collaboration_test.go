package policy

import (
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestCollaborationDefaultPreservesExistingPolicyVersions(t *testing.T) {
	p, err := Normalize(protocol.Policy{Mode: "focused"})
	const originalVersion = "58145bc95f84be1388620badc420d35c2c2a554924c735380df69db7d5285617"
	if err != nil || Version(p) != originalVersion || CollaborationMode(p) != "default" {
		t.Fatalf("default policy changed the pre-feature fingerprint: %+v %s %v", p, Version(p), err)
	}
	p.CollaborationMode = "default"
	p, err = Normalize(p)
	if err != nil || p.CollaborationMode != "" || Version(p) != originalVersion {
		t.Fatal("explicit default was not canonicalized to the existing policy", p, err)
	}
	p.CollaborationMode = "plan"
	p, err = Normalize(p)
	if err != nil || CollaborationMode(p) != "plan" || Version(p) == originalVersion || !strings.Contains(Instructions(p), "本回合不实施代码或配置修改") || !strings.Contains(Instructions(p), "方案讨论") {
		t.Fatal("planning intent was not included in the versioned task instructions", p, err)
	}
	p.CollaborationMode = "unsupported"
	if _, err = Normalize(p); err == nil {
		t.Fatal("an unknown collaboration mode was accepted")
	}
}
