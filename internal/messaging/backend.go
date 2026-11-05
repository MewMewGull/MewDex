package messaging

import (
	"errors"
	"slices"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

var ErrHubUnavailable = errors.New("Hub is temporarily unavailable")

// Backend separates Hub state from the APP's durable channel inbox and outbox.
type Backend interface {
	Get(string, string, any) error
	Configure(string, string, string, bool) (string, error)
	Identity(string) (protocol.Identity, error)
	Result(string) (protocol.CommandResult, error)
	EventPage(string, int64, int) (protocol.EventPage, error)
	Events(int64, string, int) ([]protocol.Event, error)
	QueueItems(string) ([]protocol.QueueItem, error)
	Interaction(string, string) (protocol.Interaction, error)
}

type storeBackend struct{ *store.Store }

func (b storeBackend) Interaction(session, id string) (protocol.Interaction, error) {
	var request protocol.Interaction
	err := b.Store.Get("interactions", id, &request)
	if err == nil && request.SessionID != session {
		err = store.ErrNotFound
	}
	return request, err
}

func (b storeBackend) Get(bucket, id string, out any) error {
	if err := b.Store.Get(bucket, id, out); err != nil {
		return err
	}
	if device, ok := out.(*protocol.Device); ok && slices.Contains(device.Roles, "coordinator") {
		projects, err := b.HubProjects()
		device.Projects = projects
		return err
	}
	return nil
}

func (b storeBackend) Configure(device, project, previous string, enabled bool) (string, error) {
	access := protocol.Access{Targets: []protocol.TargetAccess{}}
	if enabled {
		access.Targets = append(access.Targets, protocol.TargetAccess{DeviceID: device, ProjectIDs: []string{project}})
	}
	if previous != "" {
		_, err := b.Identity(previous)
		if err == nil {
			return previous, b.UpdateAccess(previous, access)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	if !enabled {
		return "", nil
	}
	code, err := b.InviteWithAccess("client", access, time.Minute)
	if err != nil {
		return "", err
	}
	_, identity, err := b.Pair(code, "消息渠道", "")
	return identity.ID, err
}
