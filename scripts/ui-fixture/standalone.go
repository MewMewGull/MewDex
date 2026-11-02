package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/executor"
	"mewdex/internal/hub"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

// A private development network uses the production Hub, command ledger and
// executor outbox. It has no gateway, model credentials or usable engine.
func serveStandalone(ctx context.Context, c config.Config, state *store.Store, x *executor.Executor, device protocol.Device, root string) error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("standalone UI fixture requires an explicit loopback listener")
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	hubState, err := store.Open(filepath.Join(c.DataDir, "standalone-hub.db"))
	if err != nil {
		return err
	}
	defer hubState.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h := hub.New(c, hubState)
	h.SetContext(ctx)
	h.SetLocal(x)
	if err := h.Device(device); err != nil {
		return err
	}
	code, err := hubState.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: device.ID, ProjectIDs: []string{c.Projects[0].ID}}}}, 30*time.Minute)
	if err != nil {
		return err
	}
	invitation, err := os.OpenFile(filepath.Join(root, "input-invitation"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = invitation.WriteString(code); err != nil {
		invitation.Close()
		return err
	}
	if err = invitation.Close(); err != nil {
		return err
	}
	mirror := func() error {
		events, err := state.Unacked()
		if err != nil {
			return err
		}
		for _, event := range events {
			if _, err := h.Ingest(event); err != nil {
				return err
			}
			if err := state.Ack(event.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if err = mirror(); err != nil {
		return err
	}
	server := &http.Server{Handler: h.Handler(), ReadHeaderTimeout: 5 * time.Second}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		cancel()
		stop, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-finished:
			return err
		case <-ticker.C:
			if err := mirror(); err != nil {
				return err
			}
		}
	}
}
