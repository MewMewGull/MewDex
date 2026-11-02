package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fixtureBackend struct {
	entry       Entry
	definition  Definition
	installs    int
	removes     int
	starts      int
	lostInstall bool
	lostRemove  bool
}

func (*fixtureBackend) Scope() string { return "fixture" }
func (b *fixtureBackend) Inspect(_ context.Context, d Definition) (Entry, error) {
	entry := b.entry
	entry.Matches = b.definition.Program == d.Program
	return entry, nil
}
func (b *fixtureBackend) Install(_ context.Context, d Definition) error {
	b.installs++
	b.definition = d
	b.entry = Entry{Present: true, Owner: d.Owner, Enabled: true, Raw: []byte("original OS entry")}
	if b.lostInstall {
		return errors.New("lost install response")
	}
	return nil
}
func (b *fixtureBackend) Remove(context.Context, Definition) error {
	b.removes++
	b.entry = Entry{}
	if b.lostRemove {
		return errors.New("lost removal response")
	}
	return nil
}
func (b *fixtureBackend) Start(context.Context, Definition) error { b.starts++; return nil }
func (*fixtureBackend) Stop(context.Context, Definition) error    { return nil }

func fixtureController(t *testing.T) (*Controller, *fixtureBackend, string, Binding) {
	t.Helper()
	root := t.TempDir()
	b := &fixtureBackend{}
	c, err := New(root, b)
	if err != nil {
		t.Fatal(err)
	}
	return c, b, filepath.Join(root, "runtime", "node"), Binding{DeviceID: "device", ProfileSHA256: Fingerprint([]byte("profile"))}
}

func TestStartupPreservesForeignEntryWithoutChangingItsRegistration(t *testing.T) {
	c, b, program, binding := fixtureController(t)
	b.entry = Entry{Present: true, Owner: "another-application", Enabled: true, Raw: []byte("foreign entry")}
	for _, enabled := range []bool{true, false} {
		if _, err := c.Apply(context.Background(), enabled, program, binding); err == nil {
			t.Fatal("foreign startup entry was accepted")
		}
	}
	if b.installs != 0 || b.removes != 0 || string(b.entry.Raw) != "foreign entry" {
		t.Fatal("foreign startup registration was changed")
	}
}

func TestStartupRecoversAcceptedInstallAndRemovalWithoutReplayingThem(t *testing.T) {
	c, b, program, binding := fixtureController(t)
	ctx := context.Background()
	b.lostInstall = true
	if _, err := c.Apply(ctx, true, program, binding); err == nil {
		t.Fatal("lost installation response reported success")
	}
	state, err := c.Status(ctx, binding)
	if err != nil || !state.Pending || !state.Enabled {
		t.Fatal("accepted pending registration could not be observed")
	}
	if _, err = c.Apply(ctx, true, program, binding); err != nil || b.installs != 1 {
		t.Fatal("accepted registration was installed again")
	}
	record, _, err := c.read()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := c.Authorized(record.Definition.Owner, program, binding); err != nil || !ok {
		t.Fatal("current registered program was not authorized")
	}
	b.lostRemove = true
	if _, err = c.Apply(ctx, false, program, binding); err == nil {
		t.Fatal("lost removal response reported success")
	}
	if ok, _ := c.Authorized(record.Definition.Owner, program, binding); ok {
		t.Fatal("disabled intent allowed automatic startup")
	}
	if _, err = c.Apply(ctx, false, program, binding); err != nil || b.removes != 1 {
		t.Fatal("accepted removal was executed again")
	}
	retained, _ := filepath.Glob(filepath.Join(c.root, "startup-history", "*.entry"))
	if len(retained) != 1 {
		t.Fatal("owned OS entry was not preserved before removal")
	}
	raw, _ := os.ReadFile(retained[0])
	if string(raw) != "original OS entry" {
		t.Fatal("preserved OS entry changed")
	}
}

func TestStartupProfileOrProgramChangeRequiresExplicitReview(t *testing.T) {
	c, b, program, binding := fixtureController(t)
	ctx := context.Background()
	if _, err := c.Apply(ctx, true, program, binding); err != nil {
		t.Fatal(err)
	}
	record, _, _ := c.read()
	changed := binding
	changed.ProfileSHA256 = Fingerprint([]byte("restored profile"))
	if err := c.Start(ctx, changed); err == nil || b.starts != 0 {
		t.Fatal("restored profile automatically started")
	}
	if ok, _ := c.Authorized(record.Definition.Owner, program, changed); ok {
		t.Fatal("old entry authorized a changed profile")
	}
	if ok, _ := c.Authorized(record.Definition.Owner, program+"-old", binding); ok {
		t.Fatal("old program remained authorized")
	}
	state, err := c.Apply(ctx, true, program, changed)
	if err != nil || !state.MatchesProfile {
		t.Fatal("explicit review did not adopt the new binding")
	}
	if err = c.Start(ctx, changed); err != nil || b.starts != 1 {
		t.Fatal("reviewed profile could not start")
	}
}

func TestStartupRejectsProgramOutsideTheInstallation(t *testing.T) {
	c, b, _, binding := fixtureController(t)
	if _, err := c.Apply(context.Background(), true, filepath.Join(t.TempDir(), "foreign-program"), binding); err == nil || b.installs != 0 {
		t.Fatal("external program was registered")
	}
}
