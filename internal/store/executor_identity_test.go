package store

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecutorHostUniquenessAndInvitationRollback(t *testing.T) {
	s := memory(t)
	host := strings.Repeat("a", 64)
	code, _ := s.Invite("executor", time.Minute)
	_, original, err := s.PairWithHost(code, "Mac", "mac", host)
	if err != nil {
		t.Fatal(err)
	}
	code, _ = s.Invite("executor", time.Minute)
	for _, candidate := range []struct{ device, host string }{{"mac", host}, {"new-profile", host}, {"empty-host", ""}, {"invalid-host", "raw-machine-id"}} {
		if _, _, err = s.PairWithHost(code, "Mac", candidate.device, candidate.host); err == nil {
			t.Fatal("duplicate or unverified host was paired")
		}
	}
	if _, _, err = s.PairWithHost(code, "other", "other", strings.Repeat("b", 64)); err != nil {
		t.Fatal("rejected pairing consumed its invitation", err)
	}
	if err = s.BindExecutorHost(original.ID, "mac", strings.Repeat("b", 64)); err == nil {
		t.Fatal("credential moved to a different host")
	}
}

func TestLegacyExecutorHostBindingIsAtomic(t *testing.T) {
	s := memory(t)
	code, _ := s.Invite("executor", time.Minute)
	_, first, _ := s.Pair(code, "old profile", "first")
	code, _ = s.Invite("executor", time.Minute)
	_, second, _ := s.Pair(code, "new profile", "second")
	host := strings.Repeat("c", 64)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, identity := range []struct{ id, device string }{{first.ID, first.DeviceID}, {second.ID, second.DeviceID}} {
		group.Go(func() { results <- s.BindExecutorHost(identity.id, identity.device, host) })
	}
	group.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("host binding admitted multiple executor identities", accepted)
	}
	identities, err := s.Identities()
	if err != nil || len(identities) != 2 {
		t.Fatal("binding changed account membership", err)
	}
}

func TestLocalInputLoginReusesIdentityWithoutRegisteringDevices(t *testing.T) {
	s := memory(t)
	var previous string
	for range 4 {
		token, identity, err := s.LocalInputKey("hub-device", "desktop input")
		if err != nil || previous != "" && previous != identity.ID {
			t.Fatal("local login generated another account", err)
		}
		verified, err := s.Authenticate(token)
		if err != nil || verified.ID != identity.ID {
			t.Fatal("login credential does not match its identity", err)
		}
		previous = identity.ID
	}
	identities, _ := s.Identities()
	devices, _ := s.List("devices")
	if len(identities) != 1 || len(devices) != 0 {
		t.Fatal("local input login duplicated identities or registered execution devices")
	}
}
