package localnode

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/serviceport"
)

func TestPortChoiceIsExplicitAndConflictFailsBeforeStarting(t *testing.T) {
	m := testManager(t)
	m.options.StartupBackend = &startupBackendFixture{}
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	if setup.Snapshot.Listen != "127.0.0.1:47830" {
		t.Fatal("default listener changed", setup.Snapshot.Listen)
	}
	startup, err := m.Handle(ctx, Request{Operation: "startup.apply", Startup: &StartupRequest{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, rawPort, _ := net.SplitHostPort(occupied.Addr().String())
	port, _ := strconv.Atoi(rawPort)
	updated, err := m.Handle(ctx, Request{Operation: "service.port", ExpectedDeviceID: setup.Snapshot.DeviceID, Listener: &ListenerRequest{Port: port}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Snapshot.Listen != occupied.Addr().String() || updated.Snapshot.DeviceID != setup.Snapshot.DeviceID || updated.Snapshot.State != "stopped" {
		t.Fatal("saving the chosen port changed identity or started a service", updated.Snapshot)
	}
	registered, err := m.Handle(ctx, Request{Operation: "startup.status"})
	if err != nil || !registered.Startup.MatchesProfile || !registered.Startup.EntryMatches || registered.Startup.Name != startup.Startup.Name {
		t.Fatal("port change broke or duplicated the startup entry", err)
	}
	before, _ := os.ReadFile(m.profile())
	started := time.Now()
	_, err = m.Handle(ctx, Request{Operation: "start", ExpectedDeviceID: setup.Snapshot.DeviceID})
	var conflict *serviceport.OccupiedError
	if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "端口 "+rawPort+" 已被占用") {
		t.Fatal("occupied port did not produce the actionable error", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("port conflict waited for startup timeout")
	}
	after, _ := os.ReadFile(m.profile())
	if string(before) != string(after) {
		t.Fatal("conflict silently changed configuration")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Read(c.DataDir); err == nil {
		t.Fatal("conflict started a service")
	}
}

func TestPortEditsRejectInvalidValuesAndRunningInstallation(t *testing.T) {
	m := testManager(t)
	m.options.StartupBackend = &startupBackendFixture{}
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.profile())
	for _, port := range []int{-1, 0, 65536} {
		if _, err := m.Handle(ctx, Request{Operation: "service.port", ExpectedDeviceID: setup.Snapshot.DeviceID, Listener: &ListenerRequest{Port: port}}); err == nil {
			t.Fatal("accepted invalid port", port)
		}
	}
	if _, err := m.Handle(ctx, Request{Operation: "service.port", Listener: &ListenerRequest{Port: 47831}}); err == nil {
		t.Fatal("accepted edit without device identity")
	}
	lease, err := lifecycle.Installation(m.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Handle(ctx, Request{Operation: "service.port", ExpectedDeviceID: setup.Snapshot.DeviceID, Listener: &ListenerRequest{Port: 47831}})
	_ = lease.Close()
	if err == nil {
		t.Fatal("changed listener of a running installation")
	}
	after, _ := os.ReadFile(m.profile())
	if string(before) != string(after) {
		t.Fatal("rejected edit changed configuration")
	}
	c, _ := config.Read(m.profile())
	if c.Listen != "127.0.0.1:47830" {
		t.Fatal("rejected edit changed the default port")
	}
}
