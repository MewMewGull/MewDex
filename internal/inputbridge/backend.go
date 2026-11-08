// Package inputbridge runs message platform connections inside an input-owned service.
package inputbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type backend struct {
	ctx    context.Context
	base   string
	token  string
	store  *store.Store
	client *http.Client
}

func (b *backend) request(ctx context.Context, token, method, path string, body, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(protocol.JSON(body)))
	if err != nil {
		return errors.New("invalid Hub request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := b.client.Do(request)
	if err != nil {
		return messaging.ErrHubUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return store.ErrNotFound
	}
	if response.StatusCode >= 500 || response.StatusCode == 429 {
		return messaging.ErrHubUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Hub rejected the channel request (HTTP " + strconv.Itoa(response.StatusCode) + ")")
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(out); err != nil {
		return messaging.ErrHubUnavailable
	}
	return nil
}

func (b *backend) get(path string, out any) error {
	return b.request(b.ctx, b.token, "GET", path, nil, out)
}

func (b *backend) Get(bucket, id string, out any) error {
	switch bucket {
	case "devices":
		var devices []protocol.Device
		if err := b.get("/v1/devices", &devices); err != nil {
			return err
		}
		for _, device := range devices {
			if device.ID == id {
				return json.Unmarshal(protocol.JSON(device), out)
			}
		}
	case "sessions":
		var sessions []protocol.Session
		if err := b.get("/v1/sessions", &sessions); err != nil {
			return err
		}
		for _, session := range sessions {
			if session.ID == id {
				return json.Unmarshal(protocol.JSON(session), out)
			}
		}
	default:
		return errors.New("unsupported Hub projection")
	}
	return store.ErrNotFound
}

func (b *backend) Identity(id string) (protocol.Identity, error) {
	var identities []protocol.Identity
	err := b.get("/v1/identities", &identities)
	if err != nil {
		return protocol.Identity{}, err
	}
	for _, identity := range identities {
		if identity.ID == id {
			return identity, nil
		}
	}
	return protocol.Identity{}, store.ErrNotFound
}

func (b *backend) Configure(device, project, previous string, enabled bool) (string, error) {
	access := protocol.Access{Targets: []protocol.TargetAccess{}}
	if enabled {
		access.Targets = append(access.Targets, protocol.TargetAccess{DeviceID: device, ProjectIDs: []string{project}})
	}
	if previous != "" {
		_, err := b.Identity(previous)
		if err == nil {
			return previous, b.request(b.ctx, b.token, "PUT", "/v1/identities/"+url.PathEscape(previous), access, nil)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	if !enabled {
		return "", nil
	}
	var invitation struct {
		Code string `json:"code"`
	}
	if err := b.request(b.ctx, b.token, "POST", "/v1/invitations", map[string]any{"role": "client", "access": access}, &invitation); err != nil {
		return "", err
	}
	var paired struct {
		Token    string            `json:"token"`
		Identity protocol.Identity `json:"identity"`
	}
	if err := b.request(b.ctx, "", "POST", "/v1/pair", map[string]string{"code": invitation.Code, "name": "APP 消息渠道"}, &paired); err != nil {
		return "", err
	}
	if paired.Token == "" || paired.Identity.Role != "client" {
		return "", errors.New("invalid channel identity")
	}
	if err := b.store.Put("input_credentials", paired.Identity.ID, paired.Token); err != nil {
		return "", err
	}
	return paired.Identity.ID, nil
}

func (b *backend) Submit(ctx context.Context, identity string, command protocol.Command) (protocol.CommandResult, error) {
	var token string
	var result protocol.CommandResult
	if err := b.store.Get("input_credentials", identity, &token); err != nil {
		return result, err
	}
	err := b.request(ctx, token, "POST", "/v1/commands", command, &result)
	return result, err
}

func (b *backend) Result(id string) (protocol.CommandResult, error) {
	var result protocol.CommandResult
	err := b.get("/v1/commands/"+url.PathEscape(id), &result)
	return result, err
}

func (b *backend) EventPage(session string, before int64, limit int) (protocol.EventPage, error) {
	var page protocol.EventPage
	query := url.Values{"sessionId": {session}, "before": {strconv.FormatInt(before, 10)}, "limit": {strconv.Itoa(limit)}}
	err := b.get("/v1/session-events?"+query.Encode(), &page)
	return page, err
}

func (b *backend) Events(after int64, session string, limit int) ([]protocol.Event, error) {
	var page protocol.EventPage
	query := url.Values{"sessionId": {session}, "after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(limit)}}
	err := b.get("/v1/session-events?"+query.Encode(), &page)
	return page.Events, err
}

func (b *backend) QueueItems(session string) ([]protocol.QueueItem, error) {
	var items []protocol.QueueItem
	err := b.get("/v1/queue?sessionId="+url.QueryEscape(session), &items)
	return items, err
}

func (b *backend) Interaction(session, id string) (protocol.Interaction, error) {
	var items []protocol.Interaction
	if err := b.get("/v1/interactions?sessionId="+url.QueryEscape(session), &items); err != nil {
		return protocol.Interaction{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return protocol.Interaction{}, store.ErrNotFound
}

func newBackend(ctx context.Context, base, token string, s *store.Store) *backend {
	return &backend{ctx: ctx, base: strings.TrimRight(base, "/"), token: token, store: s, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Hub redirects are disabled") }}}
}
