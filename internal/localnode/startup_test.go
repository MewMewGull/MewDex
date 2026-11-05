package localnode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/serviceguard"
	"mewdex/internal/startup"
)

type startupBackendFixture struct {
	entry   startup.Entry
	program string
}

func (*startupBackendFixture) Scope() string { return "fixture" }
func (b *startupBackendFixture) Inspect(_ context.Context, d startup.Definition) (startup.Entry, error) {
	entry := b.entry
	entry.Matches = b.program == d.Program
	return entry, nil
}
func (b *startupBackendFixture) Install(_ context.Context, d startup.Definition) error {
	b.program = d.Program
	b.entry = startup.Entry{Present: true, Owner: d.Owner, Enabled: true}
	return nil
}
func (b *startupBackendFixture) Remove(context.Context, startup.Definition) error {
	b.entry = startup.Entry{}
	return nil
}
func (*startupBackendFixture) Start(context.Context, startup.Definition) error { return nil }
func (*startupBackendFixture) Stop(context.Context, startup.Definition) error  { return nil }

func TestManagedStartupOwnsTheInstallationAndSuspendsChangedProfiles(t *testing.T) {
	m := testManager(t)
	backend := &startupBackendFixture{}
	m.options.StartupBackend = backend
	ctx := context.Background()
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Name: "startup fixture", Hub: true, Listen: "127.0.0.1:0"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Handle(ctx, Request{Operation: "startup.apply", Startup: &StartupRequest{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(m.options.Root, "startup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record startup.Record
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	driver, err := New(Options{Root: m.options.Root, Program: record.Definition.Program, StartupBackend: backend})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	sentinel := errors.New("fixture exits without creating a service")
	err = driver.RunManaged(ctx, record.Definition.Owner, func(ctx context.Context, c config.Config) error {
		called = true
		if c.InstallationRoot != "" {
			t.Fatal("foreground callback would acquire its installation lease twice")
		}
		if lease, err := lifecycle.Installation(m.options.Root); err == nil {
			lease.Close()
			t.Fatal("managed service did not retain the installation lease")
		}
		return serviceguard.Run(ctx, c.DataDir, nil, c.Hub || c.Executor, func(context.Context) error { return sentinel })
	})
	if !called || !errors.Is(err, sentinel) {
		t.Fatal("current entry was not run or lost its error")
	}
	lease, err := lifecycle.Installation(m.options.Root)
	if err != nil {
		t.Fatal("foreground exit retained its installation lease")
	}
	lease.Close()
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "restored or edited profile"
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = driver.RunManaged(ctx, record.Definition.Owner, func(context.Context, config.Config) error { called = true; return nil }); err != nil || called {
		t.Fatal("changed profile was automatically adopted")
	}
	status, err := m.Handle(ctx, Request{Operation: "startup.status"})
	if err != nil || status.Startup == nil || status.Startup.MatchesProfile {
		t.Fatal("changed startup binding was not visible")
	}
}
