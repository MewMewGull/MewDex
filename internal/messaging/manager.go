package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type running struct {
	adapter adapter
	cancel  context.CancelFunc
	done    chan struct{}
}

type Manager struct {
	store       *store.Store
	backend     Backend
	submit      Submit
	factory     factory
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	saveMu      sync.Mutex
	mu          sync.Mutex
	configs     map[string]channel
	status      map[string]protocol.MessagingChannel
	runs        map[string]*running
	active      map[string]bool
	stripes     [32]sync.Mutex
	login       *weixinLogin
	loginClient *http.Client
}

func New(s *store.Store, submit Submit) (*Manager, error) {
	return NewWithBackend(s, storeBackend{s}, submit)
}

func NewWithBackend(s *store.Store, backend Backend, submit Submit) (*Manager, error) {
	m := &Manager{store: s, backend: backend, submit: submit, configs: map[string]channel{}, status: map[string]protocol.MessagingChannel{}, runs: map[string]*running{}, active: map[string]bool{}}
	m.loginClient = newHTTPClient()
	m.factory = func(c channel) adapter {
		switch c.Platform {
		case "weixin":
			return newWeixin(c, s)
		case "qq":
			return newQQ(c, s)
		default:
			return newFeishu(c)
		}
	}
	for _, platform := range platforms {
		c := channel{Platform: platform, AllowedUsers: []string{}, AllowedChats: []string{}}
		if err := s.Get("messaging_channels", platform, &c); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		m.configs[platform] = c
	}
	return m, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	for _, platform := range platforms {
		m.restart(platform)
	}
	m.wg.Add(2)
	go func() { defer m.wg.Done(); m.work(m.ctx) }()
	go func() { defer m.wg.Done(); m.deliver(m.ctx) }()
}

func (m *Manager) Close() {
	m.saveMu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.saveMu.Unlock()
	m.wg.Wait()
}

// Reconnect retries channels whose Hub authorization could not be read at startup.
func (m *Manager) Reconnect() {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	for _, platform := range platforms {
		m.mu.Lock()
		missing := m.runs[platform] == nil
		m.mu.Unlock()
		if missing && m.ctx.Err() == nil {
			m.restart(platform)
		}
	}
}

func (m *Manager) config(platform string) (channel, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, exists := m.configs[platform]
	return c, exists
}

func (m *Manager) Snapshot() []protocol.MessagingChannel {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.MessagingChannel, 0, len(platforms))
	for _, platform := range platforms {
		c := m.configs[platform]
		view := m.status[platform]
		view.Platform, view.Enabled = platform, c.Enabled
		view.DeviceID, view.ProjectID = c.DeviceID, c.ProjectID
		view.AppID, view.BotUserID, view.AccountID = c.AppID, c.BotUserID, c.AccountID
		view.AllowedUsers, view.AllowedChats = append([]string{}, c.AllowedUsers...), append([]string{}, c.AllowedChats...)
		view.CredentialConfigured = c.Secret != ""
		result = append(result, view)
	}
	return result
}

func (m *Manager) setStatus(platform, state, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	view := m.status[platform]
	view.State, view.Detail = state, detail
	m.status[platform] = view
}

func (m *Manager) Update(platform string, update protocol.MessagingUpdate) error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if m.ctx != nil && m.ctx.Err() != nil {
		return errors.New("消息服务正在停止")
	}
	c, exists := m.config(platform)
	if !exists {
		return errors.New("未知消息渠道")
	}
	if len(update.AppID) > 256 || len(update.BotUserID) > 256 || strings.ContainsAny(update.AppID+update.BotUserID, "\r\n\x00") {
		return errors.New("应用和机器人 ID 无效")
	}
	if update.Secret != nil && (len(*update.Secret) > 8192 || strings.ContainsAny(*update.Secret, "\r\n\x00")) {
		return errors.New("应用密钥无效")
	}
	if c.AppID != update.AppID && update.Secret == nil && platform != "weixin" {
		c.Secret = ""
	}
	if c.ProjectID != "" && update.ProjectID == "" {
		c.BindingProjectID = c.ProjectID
	}
	c.Enabled, c.DeviceID, c.ProjectID = update.Enabled, update.DeviceID, update.ProjectID
	c.AppID, c.BotUserID = strings.TrimSpace(update.AppID), strings.TrimSpace(update.BotUserID)
	var err error
	if c.AllowedUsers, err = normalizedIDs(update.AllowedUsers); err != nil {
		return err
	}
	if c.AllowedChats, err = normalizedIDs(update.AllowedChats); err != nil {
		return err
	}
	if update.Secret != nil {
		c.Secret = strings.TrimSpace(*update.Secret)
	}
	if c.Enabled {
		var device protocol.Device
		if err = m.backend.Get("devices", c.DeviceID, &device); err != nil {
			return errors.New("请选择可用的执行设备")
		}
		found := false
		for _, role := range device.Roles {
			found = found || role == "executor"
		}
		projectFound := false
		for _, project := range device.Projects {
			projectFound = projectFound || project.ID == c.ProjectID
		}
		if !found || c.ProjectID != "" && !projectFound {
			return errors.New("请选择此执行设备上的项目")
		}
	}
	c.IdentityID, err = m.backend.Configure(c.DeviceID, c.ProjectID, c.IdentityID, c.Enabled)
	if err != nil {
		return err
	}
	return m.save(c)
}

// Call with saveMu held. A disabled channel is never restarted by credentials.
func (m *Manager) save(c channel) error {
	if err := m.store.Put("messaging_channels", c.Platform, c); err != nil {
		return err
	}
	m.mu.Lock()
	m.configs[c.Platform] = c
	m.mu.Unlock()
	if m.ctx != nil {
		m.restart(c.Platform)
	}
	return nil
}

func (m *Manager) restart(platform string) {
	m.mu.Lock()
	old := m.runs[platform]
	delete(m.runs, platform)
	c := m.configs[platform]
	m.mu.Unlock()
	if old != nil {
		old.cancel()
		<-old.done
	}
	if !c.Enabled {
		m.setStatus(platform, "disabled", "渠道已关闭")
		return
	}
	identity, err := m.backend.Identity(c.IdentityID)
	if err != nil || identity.Role != "client" || !identity.Access.Allows(c.DeviceID, c.ProjectID) {
		m.setStatus(platform, "needs_authorization", "消息输入身份已撤销；请保存渠道设置以重新授权")
		return
	}
	if c.Secret == "" || c.account() == "" {
		m.setStatus(platform, "needs_credentials", "请连接账号或填写应用凭据")
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	run := &running{adapter: m.factory(c), cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	m.runs[platform] = run
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(run.done)
		backoff := time.Second
		for ctx.Err() == nil {
			m.setRunStatus(platform, run, "connecting", "正在连接消息平台")
			err := run.adapter.Run(ctx, func(e Envelope) error { return m.receive(c, e) }, func(state, detail string) {
				m.setRunStatus(platform, run, state, detail)
			})
			if ctx.Err() != nil {
				return
			}
			_ = err // Platform transports report only sanitized diagnostics below.
			m.setRunStatus(platform, run, "retrying", "连接已中断，正在自动重连；请核对凭据、平台权限和网络")
			if !pause(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	}()
}

func (m *Manager) setRunStatus(platform string, run *running, state, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[platform] != run {
		return
	}
	view := m.status[platform]
	view.State, view.Detail = state, detail
	m.status[platform] = view
}

func (m *Manager) receive(source channel, e Envelope) error {
	if e.ID == "" || e.UserID == "" || e.ChatID == "" || len(e.Text) > 64*1024 {
		return errors.New("无效的平台消息")
	}
	c, _ := m.config(source.Platform)
	if c.account() != source.account() || c.DeviceID != source.DeviceID || c.ProjectID != source.ProjectID {
		return nil
	}
	e.Platform, e.Account = c.Platform, c.account()
	m.mu.Lock()
	view := m.status[c.Platform]
	view.LastEventAt, view.LastSenderID, view.LastChatID = protocol.Now(), e.UserID, e.ChatID
	m.status[c.Platform] = view
	m.mu.Unlock()
	if !c.accepts(e) {
		return nil
	}
	if _, err := m.authorize(c, e); err != nil {
		m.setStatus(c.Platform, "needs_authorization", "消息输入授权不可用；请检查访问权限后保存渠道设置")
		if errors.Is(err, ErrHubUnavailable) {
			return err
		}
		return nil
	}
	id := stable(c.Platform, c.account(), e.Scene, e.ChatID, e.UserID, e.ID)
	_, err := m.store.InsertObject("messaging_inbox", id, inbox{ID: id, BindingID: c.key(e), Envelope: e, State: "pending", CreatedAt: protocol.Now()})
	return err
}

func (m *Manager) authorize(c channel, e Envelope) (protocol.Identity, error) {
	current, exists := m.config(c.Platform)
	if !exists || current.IdentityID != c.IdentityID || current.account() != c.account() || current.DeviceID != c.DeviceID || current.ProjectID != c.ProjectID || !current.accepts(e) {
		return protocol.Identity{}, errors.New("消息渠道配置已更改或用户授权已撤销")
	}
	identity, err := m.backend.Identity(c.IdentityID)
	if err != nil {
		return identity, err
	}
	if identity.Role != "client" || !identity.Access.Allows(c.DeviceID, c.ProjectID) || !c.accepts(e) {
		return identity, errors.New("消息渠道已关闭或访问授权已撤销")
	}
	return identity, nil
}

func (m *Manager) authorizeDelivery(c channel, out delivery) error {
	identity, err := m.authorize(c, out.Envelope)
	if err != nil {
		return err
	}
	if c.key(out.Envelope) != out.BindingID || out.IdentityID != "" && out.IdentityID != c.IdentityID {
		return errors.New("消息回复所属渠道已更改")
	}
	if out.DeviceID == "" {
		// Replies saved by older versions belong to the channel's former project.
		out.DeviceID, out.ProjectID = c.DeviceID, c.ProjectID
		if out.ProjectID == "" {
			out.ProjectID = c.BindingProjectID
		}
	}
	if out.ProjectID != "" && (out.DeviceID != c.DeviceID || !identity.Access.Allows(out.DeviceID, out.ProjectID)) {
		return errors.New("消息回复所属项目的访问权限已撤销")
	}
	return nil
}

func (m *Manager) lock(id string) *sync.Mutex { return &m.stripes[int(id[0])%len(m.stripes)] }

func (m *Manager) work(ctx context.Context) {
	for ctx.Err() == nil {
		pending, err := m.store.PendingObjects("messaging_inbox", 100)
		if err == nil {
			for _, raw := range pending {
				var item inbox
				if json.Unmarshal(raw, &item) != nil {
					continue
				}
				m.mu.Lock()
				if m.active[item.BindingID] || len(m.active) >= 8 || ctx.Err() != nil {
					m.mu.Unlock()
					continue
				}
				m.active[item.BindingID] = true
				m.mu.Unlock()
				m.wg.Add(1)
				go func() {
					defer m.wg.Done()
					defer func() { m.mu.Lock(); delete(m.active, item.BindingID); m.mu.Unlock() }()
					guard := m.lock(item.BindingID)
					guard.Lock()
					defer guard.Unlock()
					m.process(ctx, item)
				}()
			}
		}
		m.checkEvents()
		if !pause(ctx, 300*time.Millisecond) {
			return
		}
	}
}

func (m *Manager) runCommand(ctx context.Context, identityID string, c protocol.Command) (protocol.CommandResult, error) {
	result, err := m.backend.Result(c.ID)
	if errors.Is(err, store.ErrNotFound) {
		result, err = m.submit(ctx, identityID, c)
	}
	if err != nil {
		return result, err
	}
	deadline := time.Now().Add(50 * time.Second)
	for result.State == "accepted" && time.Now().Before(deadline) && pause(ctx, 100*time.Millisecond) {
		result, err = m.backend.Result(c.ID)
		if err != nil {
			return result, err
		}
	}
	if result.State != "completed" {
		if result.Error != "" {
			return result, errors.New(result.Error)
		}
		return result, fmt.Errorf("指令结果待核实，请在 MewDex 查询 %s；不要重复提交", c.ID)
	}
	return result, nil
}

func (m *Manager) finish(item inbox, b binding, text string, scoped bool) error {
	item.State, item.Envelope.Text, item.Envelope.ReplyToken, item.Command = "done", "", "", nil
	objects := []store.Object{{Bucket: "messaging_inbox", ID: item.ID, Value: item}, {Bucket: "messaging_bindings", ID: b.ID, Value: b}}
	if text != "" {
		receipt := b
		if !scoped {
			receipt.ProjectID = ""
		}
		objects = append(objects, replyObjects(receipt, stable(item.ID, "receipt"), text)...)
	}
	return m.store.PutObjects(objects...)
}
