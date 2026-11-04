package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/gateway"
	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/webui"
)

type Runner interface {
	Handle(context.Context, protocol.Command) protocol.CommandResult
}
type peer struct {
	ctx        context.Context
	identityID string
	conn       *websocket.Conn
	device     protocol.Device
	write      sync.Mutex
	cancel     context.CancelFunc
}

func (p *peer) send(ctx context.Context, f protocol.Frame) error {
	p.write.Lock()
	defer p.write.Unlock()
	return p.conn.Write(ctx, websocket.MessageText, protocol.JSON(f))
}

type Hub struct {
	config          config.Config
	store           *store.Store
	local           Runner
	mu              sync.Mutex
	peers           map[string]*peer
	wake            chan struct{}
	attachments     *attachments.Store
	gatewayStatus   func() any
	ctx             context.Context
	subscribers     map[string]map[*websocket.Conn]bool
	revokeGateway   func(string)
	modelGateway    *gateway.Gateway
	logins          map[string]loginWindow
	messageChannels *messaging.Manager
	browserOrigins  func() []string
	webEnabled      func() bool
	gatewayRelays   map[string]*gatewayRelay
}

func New(c config.Config, s *store.Store) *Hub {
	return &Hub{config: c, store: s, peers: map[string]*peer{}, wake: make(chan struct{}), ctx: context.Background(), subscribers: map[string]map[*websocket.Conn]bool{}, gatewayRelays: map[string]*gatewayRelay{}}
}

func (h *Hub) SetAttachments(s *attachments.Store) { h.attachments = s }
func (h *Hub) SetGatewayStatus(status func() any) {
	h.mu.Lock()
	h.gatewayStatus = status
	h.mu.Unlock()
}
func (h *Hub) SetContext(ctx context.Context)            { h.ctx = ctx }
func (h *Hub) SetBrowserOrigins(origins func() []string) { h.browserOrigins = origins }
func (h *Hub) SetWebUI(enabled func() bool)              { h.webEnabled = enabled }
func (h *Hub) SetGatewayRevoker(revoke func(string)) {
	h.mu.Lock()
	h.revokeGateway = revoke
	h.mu.Unlock()
}
func (h *Hub) SetModelGateway(g *gateway.Gateway) {
	h.mu.Lock()
	h.modelGateway = g
	h.gatewayStatus = g.Snapshot
	h.revokeGateway = g.Revoke
	h.mu.Unlock()
	if h.config.Gateway != nil && h.config.Gateway.ForwardDeviceID != "" {
		g.SetForwarder(h.OpenGateway)
	}
}
func (h *Hub) SetLocal(r Runner) { h.local = r }
func (h *Hub) Device(d protocol.Device) error {
	d.LastSeen = protocol.Now()
	return h.store.RegisterDevice(d)
}
func (h *Hub) Ingest(e protocol.Event) (protocol.Event, error) {
	stored, err := h.store.Ingest(e)
	if err != nil {
		return stored, err
	}
	h.notify()
	return stored, nil
}

func (h *Hub) notify() {
	h.mu.Lock()
	close(h.wake)
	h.wake = make(chan struct{})
	h.mu.Unlock()
}

func (h *Hub) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", h.serverInfo)
	m.HandleFunc("POST /v1/pair", h.pair)
	m.HandleFunc("POST /v1/login", h.login)
	m.Handle("/v1/", h.auth(http.HandlerFunc(h.api)))
	web := webui.Handler()
	m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.webEnabled == nil || !h.webEnabled() {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "网页版尚未启动，请在桌面 APP 中启动网页版。", http.StatusServiceUnavailable)
			return
		}
		web.ServeHTTP(w, r)
	}))
	return h.cors(m)
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, err error) {
	reply(w, status, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		problem(w, 400, err)
		return false
	}
	return true
}

type identityKey struct{}

func identity(r *http.Request) protocol.Identity {
	return r.Context().Value(identityKey{}).(protocol.Identity)
}
func (h *Hub) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Path == "/v1/events/ws" {
			ticket := r.URL.Query().Get("ticket")
			h.mu.Lock()
			var t ticketRecord
			err := h.store.Get("tickets", store.Hash(ticket), &t)
			if err == nil {
				err = h.store.Delete("tickets", store.Hash(ticket))
			}
			h.mu.Unlock()
			if err != nil || time.Now().Unix() > t.Expires {
				problem(w, 401, errors.New("invalid event ticket"))
				return
			}
			id, err := h.store.Identity(t.IdentityID)
			if err != nil {
				problem(w, 401, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
			return
		}
		id, err := h.store.Authenticate(token)
		if err != nil {
			problem(w, 401, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

type ticketRecord struct {
	IdentityID string
	Expires    int64
}

func (h *Hub) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		DeviceID string `json:"deviceId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		problem(w, 400, errors.New("name required"))
		return
	}
	token, id, err := h.store.Pair(req.Code, req.Name, req.DeviceID, h.config.DeviceID)
	if err != nil {
		problem(w, 401, err)
		return
	}
	reply(w, 200, map[string]any{"token": token, "identity": id, "protocolVersion": protocol.Version})
}

func (h *Hub) api(w http.ResponseWriter, r *http.Request) {
	id := identity(r)
	if r.Method == "GET" && r.URL.Path == "/v1/me" {
		reply(w, 200, id)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/v1/gateway/relay/ws" {
		h.acceptGatewayRelay(w, r, id)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v1/gateway/authorize" {
		var request struct {
			DeviceID string `json:"deviceId"`
		}
		if !decode(w, r, &request) {
			return
		}
		grant, err := gateway.IdentityGrant(id, request.DeviceID)
		if err != nil {
			problem(w, 403, err)
			return
		}
		reply(w, 200, grant)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/v1/gateway/ws" {
		grant, err := gateway.IdentityGrant(id, id.DeviceID)
		if err != nil {
			problem(w, 403, err)
			return
		}
		h.mu.Lock()
		g := h.modelGateway
		h.mu.Unlock()
		if g == nil {
			problem(w, 503, errors.New("Hub model gateway is disabled"))
			return
		}
		g.ServeTunnel(w, r, grant, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/attachments") {
		h.attachment(w, r)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v1/file-exports" {
		h.receiveFileExport(w, r)
		return
	}
	if r.URL.Path == "/v1/node/ws" {
		if id.Role != "executor" || id.DeviceID == h.config.DeviceID {
			problem(w, 403, errors.New("executor identity required"))
			return
		}
		h.node(w, r, id)
		return
	}
	if id.Role == "executor" {
		problem(w, 403, errors.New("client identity required"))
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/messaging"):
		h.messagingAPI(w, r)
	case r.URL.Path == "/v1/mewdex/account":
		h.mewdexAccount(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/bootstrap":
		h.bootstrap(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/file-exports":
		h.outputs(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/session-events":
		h.historyPage(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/message-search":
		h.searchMessages(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/task-plan":
		h.taskPlan(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/gateway":
		if id.Role != "admin" {
			problem(w, 403, errors.New("admin required"))
			return
		}
		h.mu.Lock()
		snapshot := h.gatewayStatus
		h.mu.Unlock()
		if snapshot == nil {
			reply(w, 200, map[string]bool{"enabled": false})
		} else {
			reply(w, 200, snapshot())
		}
	case r.Method == "GET" && r.URL.Path == "/v1/devices":
		devices, err := h.visibleDevices(id)
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, devices)
	case r.Method == "GET" && r.URL.Path == "/v1/sessions":
		sessions, err := h.visibleSessions(id, r.URL.Query().Get("deviceId"))
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, sessions)
	case r.Method == "GET" && r.URL.Path == "/v1/activity":
		h.activity(w, r)
	case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		h.updateSession(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/queue":
		var session protocol.Session
		if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !id.Access.Allows(session.DeviceID, session.ProjectID) {
			problem(w, 404, errors.New("session not found"))
			return
		}
		items, err := h.store.QueueItems(session.ID)
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, items)
	case r.Method == "GET" && r.URL.Path == "/v1/policies":
		var session protocol.Session
		if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !id.Access.Allows(session.DeviceID, session.ProjectID) {
			problem(w, 404, errors.New("session not found"))
			return
		}
		values, err := store.List[protocol.PolicySnapshot](h.store, "policies")
		if err != nil {
			problem(w, 500, err)
			return
		}
		visible := []protocol.PolicySnapshot{}
		for _, value := range values {
			if value.SessionID == session.ID {
				visible = append(visible, value)
			}
		}
		sort.Slice(visible, func(i, j int) bool { return visible[i].CreatedAt > visible[j].CreatedAt })
		reply(w, 200, visible)
	case r.Method == "GET" && r.URL.Path == "/v1/interactions":
		var session protocol.Session
		if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !id.Access.Allows(session.DeviceID, session.ProjectID) {
			problem(w, 404, errors.New("session not found"))
			return
		}
		requests, err := store.List[protocol.Interaction](h.store, "interactions")
		if err != nil {
			problem(w, 500, err)
			return
		}
		pending := []protocol.Interaction{}
		for _, request := range requests {
			if request.SessionID == session.ID && request.State == "pending" {
				pending = append(pending, interactionForIdentity(id, request))
			}
		}
		reply(w, 200, pending)
	case r.Method == "GET" && r.URL.Path == "/v1/events":
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil && r.URL.Query().Get("after") != "" {
			problem(w, 400, err)
			return
		}
		events, err := h.store.Events(after, r.URL.Query().Get("sessionId"), 500)
		if err != nil {
			problem(w, 500, err)
			return
		}
		visible := []protocol.Event{}
		for _, event := range events {
			if h.visibleEvent(id, event) {
				view, err := h.inputEvent(id, event)
				if err != nil {
					problem(w, 500, err)
					return
				}
				visible = append(visible, view)
			}
		}
		if len(events) > 0 {
			w.Header().Set("X-MewDex-Cursor", strconv.FormatInt(events[len(events)-1].Sequence, 10))
		}
		reply(w, 200, visible)
	case r.Method == "POST" && r.URL.Path == "/v1/events/ticket":
		ticket := protocol.ID()
		rec := ticketRecord{IdentityID: id.ID, Expires: time.Now().Add(time.Minute).Unix()}
		if err := h.store.Put("tickets", store.Hash(ticket), rec); err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, map[string]string{"ticket": ticket})
	case r.Method == "GET" && r.URL.Path == "/v1/events/ws":
		h.events(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/commands":
		h.command(w, r, id)
	case r.Method == "GET" && r.URL.Path == "/v1/pending-commands":
		h.pendingCommands(w, r)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/commands/"):
		commandID := strings.TrimPrefix(r.URL.Path, "/v1/commands/")
		command, err := h.store.Command(commandID)
		if err != nil || !h.allowsCommand(id, command) {
			problem(w, 404, errors.New("command not found"))
			return
		}
		result, err := h.store.Result(commandID)
		if err != nil {
			problem(w, 404, err)
			return
		}
		h.replyResult(w, 200, result)
	case r.Method == "POST" && r.URL.Path == "/v1/invitations":
		if id.Role != "admin" {
			problem(w, 403, errors.New("admin required"))
			return
		}
		var req struct {
			Role   string           `json:"role"`
			Access *protocol.Access `json:"access"`
		}
		if !decode(w, r, &req) {
			return
		}
		access := protocol.DefaultAccess(req.Role)
		if req.Access != nil {
			access = *req.Access
		}
		if err := validateAccess(access); err != nil {
			problem(w, 400, err)
			return
		}
		code, err := h.store.InviteWithAccess(req.Role, access, 10*time.Minute)
		if err != nil {
			problem(w, 400, err)
			return
		}
		reply(w, 201, map[string]string{"code": code})
	case r.Method == "GET" && r.URL.Path == "/v1/identities":
		if id.Role != "admin" {
			problem(w, 403, errors.New("admin required"))
			return
		}
		identities, err := h.store.Identities()
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, identities)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v1/identities/"):
		if id.Role != "admin" {
			problem(w, 403, errors.New("admin required"))
			return
		}
		target := strings.TrimPrefix(r.URL.Path, "/v1/identities/")
		var access protocol.Access
		if !decode(w, r, &access) {
			return
		}
		if err := validateAccess(access); err != nil {
			problem(w, 400, err)
			return
		}
		if err := h.store.UpdateAccess(target, access); err != nil {
			problem(w, 400, err)
			return
		}
		h.disconnectIdentity(target)
		reply(w, 200, map[string]bool{"updated": true})
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1/identities/"):
		if id.Role != "admin" {
			problem(w, 403, errors.New("admin required"))
			return
		}
		target := strings.TrimPrefix(r.URL.Path, "/v1/identities/")
		if err := h.store.Revoke(target); err != nil {
			problem(w, 500, err)
			return
		}
		h.disconnectIdentity(target)
		reply(w, 200, map[string]bool{"revoked": true})
	default:
		problem(w, 404, errors.New("unknown API operation"))
	}
}

func (h *Hub) attachment(w http.ResponseWriter, r *http.Request) {
	if h.attachments == nil {
		problem(w, 503, errors.New("attachment store unavailable"))
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v1/attachments" {
		if identity(r).Role == "executor" {
			problem(w, 403, errors.New("client identity required"))
			return
		}
		device, project := r.URL.Query().Get("deviceId"), r.URL.Query().Get("projectId")
		if device == "" || project == "" || !identity(r).Access.Allows(device, project) {
			problem(w, 403, errors.New("an authorized target device and project are required"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, attachments.MaxSize+1)
		defer r.Body.Close()
		meta, err := h.attachments.SaveTarget(r.URL.Query().Get("name"), r.Body, identity(r).ID, device, project)
		if err != nil {
			problem(w, 400, err)
			return
		}
		reply(w, 201, meta)
		return
	}
	if r.Method != "GET" {
		problem(w, 405, errors.New("unsupported attachment operation"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/attachments/")
	blob := strings.HasSuffix(path, "/content")
	id := strings.TrimSuffix(path, "/content")
	meta, err := h.attachments.Metadata(id)
	if err != nil {
		problem(w, 404, errors.New("attachment not found"))
		return
	}
	actor := identity(r)
	allowed := actor.Access.Allows(meta.TargetDeviceID, meta.TargetProjectID)
	if actor.Role == "executor" {
		allowed = meta.TargetDeviceID == actor.DeviceID
	}
	if !allowed {
		problem(w, 404, errors.New("attachment not found"))
		return
	}
	if !blob {
		reply(w, 200, meta)
		return
	}
	file, err := h.attachments.Open(id)
	if err != nil {
		problem(w, 404, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(meta.Name), time.Time{}, file)
}

func (h *Hub) command(w http.ResponseWriter, r *http.Request, id protocol.Identity) {
	var c protocol.Command
	if !decode(w, r, &c) {
		return
	}
	if c.ID == "" || c.DeviceID == "" {
		problem(w, 400, errors.New("id and deviceId required"))
		return
	}
	c.SourceID = id.ID
	c.CreatedAt = protocol.Now()
	if !h.allowsCommand(id, c) {
		problem(w, 403, errors.New("target project is outside this input's access scope"))
		return
	}
	if !h.allowsInteractionAcceptance(id, c) {
		problem(w, 403, errors.New("this input cannot approve the requested execution permissions"))
		return
	}
	projectID := c.ProjectID
	if c.SessionID != "" {
		var session protocol.Session
		if h.store.Get("sessions", c.SessionID, &session) == nil {
			projectID = session.ProjectID
		}
	}
	for _, item := range c.Input {
		if item.AttachmentID == "" {
			continue
		}
		if h.attachments == nil {
			problem(w, 503, errors.New("attachment store unavailable"))
			return
		}
		meta, err := h.attachments.Metadata(item.AttachmentID)
		if err != nil || !id.Access.Allows(meta.TargetDeviceID, meta.TargetProjectID) {
			problem(w, 403, errors.New("attachment is outside this input's scope"))
			return
		}
		if meta.TargetDeviceID != "" && meta.TargetDeviceID != c.DeviceID {
			problem(w, 400, errors.New("attachment was uploaded for another execution device"))
			return
		}
		if meta.TargetProjectID != "" && meta.TargetProjectID != projectID {
			problem(w, 400, errors.New("attachment was uploaded for another project"))
			return
		}
	}
	if c.SessionID != "" {
		var s protocol.Session
		if err := h.store.Get("sessions", c.SessionID, &s); err != nil || s.DeviceID != c.DeviceID {
			problem(w, 400, errors.New("session does not belong to target device"))
			return
		}
	}
	result, owner, err := h.store.Claim(c)
	if err != nil {
		problem(w, 409, err)
		return
	}
	if !owner {
		h.replyResult(w, 200, result)
		return
	}
	if c.Kind == "workspace.update" {
		out, updateErr := h.store.UpdateProjectMetadata(c)
		if updateErr != nil {
			out = protocol.CommandResult{CommandID: c.ID, State: "failed", Error: updateErr.Error()}
			if err = h.store.Finish(out); err != nil {
				problem(w, 500, err)
				return
			}
		}
		h.notify()
		h.replyResult(w, 200, out)
		return
	}
	if h.unsupportedCollaboration(c) {
		result.State = "failed"
		result.Error = "executor does not support planning mode; update the executor before using it"
		if err = h.store.Finish(result); err != nil {
			problem(w, 500, err)
			return
		}
		h.replyResult(w, 202, result)
		return
	}
	h.mu.Lock()
	remote := h.peers[c.DeviceID]
	h.mu.Unlock()
	if remote != nil && slices.Contains(remote.device.Roles, "gateway") && !slices.Contains(remote.device.Roles, "executor") {
		result.State = "failed"
		result.Error = "target device does not provide task execution"
		_ = h.store.Finish(result)
		h.replyResult(w, 202, result)
		return
	}
	if c.DeviceID == h.config.DeviceID && h.local != nil {
		go func() {
			ctx, cancel := context.WithTimeout(h.ctx, 45*time.Second)
			defer cancel()
			out := h.local.Handle(ctx, c)
			_ = h.store.Finish(out)
		}()
	} else if remote != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err = remote.send(ctx, protocol.Frame{Version: protocol.Version, Type: "command", ID: c.ID, Payload: protocol.JSON(c)})
		cancel()
		if err != nil {
			result.State = "unknown"
			result.Error = "connection failed during delivery; outcome not confirmed"
			_ = h.store.Finish(result)
		}
	} else {
		result.State = "failed"
		result.Error = "executor is offline; command was not executed"
		_ = h.store.Finish(result)
	}
	h.replyResult(w, 202, result)
}

func (h *Hub) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := false
			for _, candidate := range h.config.AllowedOrigins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if !allowed && h.browserOrigins != nil {
				for _, candidate := range h.browserOrigins() {
					if candidate == origin {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				problem(w, 403, errors.New("origin is not allowed"))
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "X-MewDex-Cursor")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Hub) events(w http.ResponseWriter, r *http.Request) {
	kinds, err := eventKinds(r.URL.Query())
	if err != nil {
		problem(w, 400, err)
		return
	}
	sessionID, scoped := r.URL.Query().Get("sessionId"), r.URL.Query().Has("sessionId")
	if sessionID != "" {
		var session protocol.Session
		if h.store.Get("sessions", sessionID, &session) != nil || !identity(r).Access.Allows(session.DeviceID, session.ProjectID) {
			problem(w, 404, errors.New("session not found"))
			return
		}
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	unregister := h.registerSubscriber(identity(r).ID, conn)
	defer unregister()
	ctx := conn.CloseRead(r.Context())
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	for {
		currentIdentity, err := h.store.Identity(identity(r).ID)
		if err != nil {
			return
		}
		h.mu.Lock()
		wake := h.wake
		h.mu.Unlock()
		events, err := h.store.Events(after, "", 500)
		if err != nil {
			return
		}
		for _, e := range events {
			after = e.Sequence
			if !h.visibleEvent(currentIdentity, e) {
				continue
			}
			e, include := activityStreamEvent(e, sessionID, scoped)
			if !include || kinds != nil && !kinds[e.Kind] {
				continue
			}
			view, err := h.inputEvent(currentIdentity, e)
			if err != nil {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = conn.Write(writeCtx, websocket.MessageText, protocol.JSON(view))
			cancel()
			if err != nil {
				return
			}
		}
		if len(events) > 0 {
			checkpoint := protocol.Event{Sequence: after, ID: "checkpoint-" + strconv.FormatInt(after, 10), Kind: "stream.checkpoint", Payload: protocol.JSON(map[string]any{}), CreatedAt: protocol.Now()}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = conn.Write(writeCtx, websocket.MessageText, protocol.JSON(checkpoint))
			cancel()
			if err != nil {
				return
			}
		}
		if len(events) == 500 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(20 * time.Second):
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Hub) node(w http.ResponseWriter, r *http.Request, id protocol.Identity) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 * 1024 * 1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	_, b, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var hello protocol.Frame
	if json.Unmarshal(b, &hello) != nil || hello.Version != protocol.Version || hello.Type != "hello" {
		return
	}
	var d protocol.Device
	if json.Unmarshal(hello.Payload, &d) != nil || d.ID != id.DeviceID {
		return
	}
	d.Online = true
	d.LastSeen = protocol.Now()
	p := &peer{ctx: ctx, conn: conn, device: d, cancel: cancel, identityID: id.ID}
	h.mu.Lock()
	prior := h.peers[d.ID]
	h.peers[d.ID] = p
	h.mu.Unlock()
	if prior != nil {
		prior.cancel()
		_ = prior.conn.CloseNow()
	}
	_ = h.Device(d)
	defer func() {
		h.mu.Lock()
		if h.peers[d.ID] == p {
			delete(h.peers, d.ID)
		}
		h.mu.Unlock()
	}()
	if err = p.send(ctx, protocol.Frame{Version: protocol.Version, Type: "hello.accepted"}); err != nil {
		return
	}
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 45*time.Second)
		_, b, err = conn.Read(readCtx)
		readCancel()
		if err != nil {
			return
		}
		if _, err = h.store.Identity(id.ID); err != nil {
			return
		}
		var f protocol.Frame
		if json.Unmarshal(b, &f) != nil || f.Version != protocol.Version {
			return
		}
		switch f.Type {
		case "gateway.error":
			h.failGatewayRelay(p, f.ID)
		case "event":
			var e protocol.Event
			if json.Unmarshal(f.Payload, &e) != nil || e.DeviceID != d.ID {
				return
			}
			if _, err = h.Ingest(e); err != nil {
				return
			}
			if err = p.send(ctx, protocol.Frame{Version: protocol.Version, Type: "event.ack", ID: e.ID}); err != nil {
				return
			}
		case "heartbeat":
			var current protocol.Device
			if h.store.Get("devices", d.ID, &current) == nil {
				d.Projects = current.Projects
			}
			d.LastSeen = protocol.Now()
			_ = h.Device(d)
			if err = p.send(ctx, protocol.Frame{Version: protocol.Version, Type: "heartbeat.ack"}); err != nil {
				return
			}
		default:
			_ = p.send(ctx, protocol.Frame{Version: protocol.Version, Type: "error", Payload: protocol.JSON(map[string]string{"error": fmt.Sprintf("unknown frame %s", f.Type)})})
		}
	}
}
