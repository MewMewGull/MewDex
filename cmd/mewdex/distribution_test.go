package main

import (
	"testing"

	"mewdex/internal/config"
)

func TestDistributionRejectsCombinedOrWrongRoleProfiles(t *testing.T) {
	previous := distributionRole
	t.Cleanup(func() { distributionRole = previous })
	for _, test := range []struct {
		role                 string
		hub, executor, valid bool
	}{
		{"hub", true, false, true},
		{"hub", true, true, false},
		{"hub", false, true, false},
		{"executor", false, true, true},
		{"executor", true, true, false},
		{"executor", true, false, false},
		{"combined", true, true, true},
	} {
		distributionRole = test.role
		err := validateDistribution(config.Config{Hub: test.hub, Executor: test.executor})
		if (err == nil) != test.valid {
			t.Fatalf("unexpected %s profile result: %v", test.role, err)
		}
	}
}
