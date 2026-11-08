package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

const helpText = "先选择项目，再发送文字开始任务。执行中收到的消息会排入下一条。\n/projects 列出可选项目\n/project 序号、项目名或项目 ID 选择或切换项目\n/help 查看用法\n/status 查看会话\n/last 获取最近一条回复\n/new 在当前项目中新建会话\n/stop 停止当前任务\n/approve 请求编号 单次确认当前范围内的命令\n/deny 请求编号 拒绝请求\n/answer 请求编号 {\"问题编号\":[\"回答\"]}\n每位用户在各聊天中独立选择项目。文件变更、额外权限和表单请在 MewDex 中处理。"

func (m *Manager) process(ctx context.Context, item inbox) {
	c, _ := m.config(item.Envelope.Platform)
	_, authorizationErr := m.authorize(c, item.Envelope)
	if errors.Is(authorizationErr, ErrHubUnavailable) {
		return
	}
	if authorizationErr != nil || c.key(item.Envelope) != item.BindingID {
		item.State, item.Envelope.Text, item.Envelope.ReplyToken, item.Command = "cancelled", "", "", nil
		_ = m.store.Put("messaging_inbox", item.ID, item)
		return
	}
	var b binding
	err := m.store.Get("messaging_bindings", item.BindingID, &b)
	if errors.Is(err, store.ErrNotFound) {
		b = binding{ID: item.BindingID, DeviceID: c.DeviceID, ProjectID: c.ProjectID}
	} else if err != nil {
		return // Keep the durable input pending during a storage failure.
	}
	b.Envelope, b.IdentityID = item.Envelope, c.IdentityID
	b.Envelope.Text = ""
	if err = m.store.Put("messaging_bindings", b.ID, b); err != nil {
		return
	}
	text, err := m.route(ctx, c, &b, &item)
	if ctx.Err() != nil || errors.Is(err, ErrHubUnavailable) {
		return // Replay only the saved command receipt after restart.
	}
	if err != nil {
		text = "消息未能完成处理：" + err.Error()
	}
	scoped := err == nil && (strings.TrimSpace(item.Envelope.Text) == "/last" || strings.TrimSpace(item.Envelope.Text) == "/status")
	if err = m.finish(item, b, text, scoped); err != nil {
		m.setStatus(c.Platform, "storage_error", "保存消息回执失败，正在保留原输入重试")
	}
}

func (m *Manager) route(ctx context.Context, c channel, b *binding, item *inbox) (string, error) {
	text := strings.TrimSpace(item.Envelope.Text)
	if text == "/help" {
		return helpText, nil
	}
	if text == "/start" {
		projects, err := m.listProjects(c, b)
		return helpText + "\n\n" + projects, err
	}
	if text == "/projects" || text == "/project" {
		return m.listProjects(c, b)
	}
	fields := strings.Fields(text)
	if len(fields) > 0 && fields[0] == "/project" {
		return m.selectProject(c, b, strings.TrimSpace(strings.TrimPrefix(text, "/project")))
	}
	control := len(fields) > 0 && (fields[0] == "/stop" || fields[0] == "/approve" || fields[0] == "/deny" || fields[0] == "/answer")
	if strings.HasPrefix(text, "/") && !control && text != "/last" && text != "/status" && text != "/new" {
		return "未知指令。\n" + helpText, nil
	}
	if b.ProjectID == "" {
		projects, err := m.listProjects(c, b)
		return "请先选择项目，再发送任务。\n" + projects, err
	}
	if _, err := m.authorizeBinding(c, *b); err != nil {
		return "", err
	}
	var session protocol.Session
	if b.SessionID != "" {
		if err := m.backend.Get("sessions", b.SessionID, &session); err != nil {
			if errors.Is(err, ErrHubUnavailable) {
				return "", err
			}
			if errors.Is(err, store.ErrNotFound) && text == "/new" {
				b.SessionID, b.CreateID, b.Cursor = "", "", 0
				return "下条消息将在当前项目中创建新的 MewDex 会话。", nil
			}
			return "", errors.New("原会话不可用，请使用 /new 新建会话")
		}
		if session.DeviceID != b.DeviceID || session.ProjectID != b.ProjectID {
			return "", errors.New("会话与当前选择的项目不匹配")
		}
	}
	if text == "/last" {
		if session.ID == "" {
			return "尚未创建会话。", nil
		}
		page, err := m.backend.EventPage(session.ID, 0, 500)
		if err != nil {
			return "", err
		}
		for index := len(page.Events) - 1; index >= 0; index-- {
			event := page.Events[index]
			if event.Kind == "engine.item/completed" && event.DeviceID == b.DeviceID {
				if reply := eventText(event); reply != "" {
					return reply, nil
				}
			}
		}
		return "此会话最近的记录中还没有完成的回复，请在 MewDex 中查看完整历史。", nil
	}
	if text == "/status" {
		if session.ID == "" {
			return "当前项目为 " + b.ProjectID + "\n尚未创建会话，发送文字即可开始。", nil
		}
		items, err := m.backend.QueueItems(session.ID)
		if err != nil {
			return "", err
		}
		count := 0
		for _, entry := range items {
			if entry.State == "queued" {
				count++
			}
		}
		return fmt.Sprintf("会话：%s\n设备：%s\n项目：%s\n状态：%s\n队列等待：%d\n暂停：%t", session.ID, b.DeviceID, b.ProjectID, session.State, count, session.Frozen), nil
	}
	if text == "/new" {
		if reason, err := m.resetBlocked(session); err != nil || reason != "" {
			return reason, err
		}
		if err := m.checkpointEvents(b); err != nil {
			return "", err
		}
		b.SessionID, b.CreateID, b.Cursor = "", "", 0
		return "下条消息将在当前项目中创建新的 MewDex 会话。", nil
	}
	if text == "" {
		return "当前支持文字消息；请将图片、语音或文件附在 MewDex 对话中。", nil
	}
	if control && session.ID == "" {
		return "尚未创建会话。", nil
	}
	if !control {
		projects, err := m.availableProjects(c, b.Envelope)
		if err != nil {
			return "", err
		}
		found := false
		for _, project := range projects {
			found = found || project.ID == b.ProjectID
		}
		if !found {
			return "当前项目已不可用，请重新选择。\n" + projectList(b, projects), nil
		}
	}
	if !control && b.SessionID == "" {
		if b.CreateID == "" {
			b.CreateID = stable(item.ID, "create")
			if err := m.store.Put("messaging_bindings", b.ID, b); err != nil {
				return "", err
			}
		}
		created, err := m.runCommand(ctx, c.IdentityID, protocol.Command{ID: b.CreateID, DeviceID: b.DeviceID, ProjectID: b.ProjectID, Kind: "create", Title: c.Platform + " · " + item.Envelope.UserID})
		if err != nil {
			return "", err
		}
		if created.Session == nil || created.Session.ID == "" {
			return "", errors.New("执行端没有返回会话编号")
		}
		session = *created.Session
		b.SessionID = session.ID
		if err = m.store.Put("messaging_bindings", b.ID, b); err != nil {
			return "", err
		}
	}
	if item.Command == nil {
		command := protocol.Command{ID: stable(item.ID, "command"), DeviceID: b.DeviceID, ProjectID: b.ProjectID, SessionID: b.SessionID}
		if control {
			if fields[0] == "/stop" {
				command.Kind, command.ExpectedTurnID = "stop", session.TurnID
			} else {
				if len(fields) < 2 {
					return "请附上当前会话的请求编号。", nil
				}
				request, err := m.backend.Interaction(b.SessionID, fields[1])
				if errors.Is(err, ErrHubUnavailable) {
					return "", err
				}
				if err != nil || request.SessionID != b.SessionID || request.State != "pending" {
					return "", errors.New("请求不属于此会话，或已经过期")
				}
				if request.Kind == "permissions" || request.Permissions != nil {
					return "额外权限请在 MewDex 管理员输入端处理。", nil
				}
				reply := protocol.InteractionReply{Action: "decline"}
				if fields[0] == "/approve" {
					if !request.CanAccept || request.ExecutionScopeID == "" || request.Kind != "command" {
						return "此请求需要在 MewDex 中查看后处理。", nil
					}
					reply.Action = "accept"
				}
				if fields[0] == "/answer" {
					if request.Kind != "questions" || !request.CanAccept {
						return "此表单请在 MewDex 中填写。", nil
					}
					for _, question := range request.Questions {
						if question.Format == "password" || question.Kind == "secret" || question.Kind == "password" {
							return "敏感表单请在 MewDex 中填写。", nil
						}
					}
					body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(text, "/answer")), fields[1]))
					reply.Action = "accept"
					if err := json.Unmarshal([]byte(body), &reply.Answers); err != nil || len(reply.Answers) == 0 {
						return "回答格式：/answer 请求编号 {\"问题编号\":[\"回答\"]}", nil
					}
				}
				command.Kind, command.RequestID, command.Response = "respond", request.ID, protocol.JSON(reply)
			}
		} else {
			if session.Archived || session.ExecutionRecovery != nil || session.StopStatus == "unconfirmed" {
				return "此会话需要在 MewDex 中恢复或核实后继续。", nil
			}
			command.Kind = "start"
			if session.State != "idle" && session.State != "failed" || session.Frozen || session.StopStatus == "background-running" {
				command.Kind = "enqueue"
			}
			command.Input = []protocol.Input{{Type: "text", Text: text}}
			command.ExpectedPolicyVersion = session.PolicyVersion
		}
		item.Command = &command
		// Persist the exact command before delivery. A crash cannot choose enqueue
		// instead of start and submit a second copy of the same user message.
		if err := m.store.Put("messaging_inbox", item.ID, item); err != nil {
			return "", err
		}
	}
	if _, err := m.authorizeBinding(c, *b); err != nil {
		return "", err
	}
	_, err := m.runCommand(ctx, c.IdentityID, *item.Command)
	if err != nil {
		return "", err
	}
	switch item.Command.Kind {
	case "enqueue":
		return "已排入下一条；若会话队列已暂停，请在 MewDex 中恢复。", nil
	case "stop":
		return "已提交停止请求；用 /status 查看执行状态。", nil
	case "respond":
		return "已提交本次回复。", nil
	}
	return "", nil
}

func eventText(event protocol.Event) string {
	switch event.Kind {
	case "engine.item/completed":
		var payload struct {
			Item struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Phase string `json:"phase"`
			} `json:"item"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Item.Type == "agentMessage" && payload.Item.Phase != "commentary" {
			return payload.Item.Text
		}
	case "interaction.updated":
		var request protocol.Interaction
		if json.Unmarshal(event.Payload, &request) != nil || request.State != "pending" {
			return ""
		}
		text := "需要你的处理：" + request.Title + "\n" + request.Message + "\n请求编号：" + request.ID
		if request.Kind == "permissions" || request.Permissions != nil || !request.CanAccept && len(request.Questions) == 0 {
			return text + "\n请在 MewDex 中查看并处理。"
		}
		if len(request.Questions) > 0 && request.Kind == "questions" {
			for _, question := range request.Questions {
				text += "\n" + question.ID + "：" + question.Title
				for _, option := range question.Options {
					text += "\n  " + option.Value + " = " + option.Label
				}
			}
			return text + "\n使用 /answer 请求编号 {\"问题编号\":[\"回答\"]}；敏感内容请在 MewDex 中填写。"
		}
		if request.Kind == "command" {
			text += "\n命令：" + request.Command + "\n目录：" + request.Cwd
		} else if request.Kind == "files" {
			// Full file previews remain in the first-party UI; never approve an
			// unseen or truncated patch from a short external notification.
			return text + "\n请在 MewDex 查看完整差异并确认。"
		}
		if request.Kind != "command" {
			return text + "\n请在 MewDex 中查看并处理。"
		}
		return text + "\n/approve " + request.ID + "\n/deny " + request.ID
	case "engine.turn/completed":
		var payload struct {
			Turn struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Turn.Status == "failed" {
			return "任务执行失败，请在 MewDex 中查看错误详情。"
		}
	}
	return ""
}

func (m *Manager) checkEvents() {
	bindings, err := store.List[binding](m.store, "messaging_bindings")
	if err != nil {
		return
	}
	for _, snapshot := range bindings {
		guard := m.lock(snapshot.ID)
		if !guard.TryLock() {
			continue
		}
		m.checkBinding(snapshot.ID)
		guard.Unlock()
	}
}

func (m *Manager) checkBinding(id string) {
	var b binding
	if m.store.Get("messaging_bindings", id, &b) != nil || b.SessionID == "" {
		return
	}
	c, _ := m.config(b.Envelope.Platform)
	if _, err := m.authorizeBinding(c, b); err != nil || c.key(b.Envelope) != b.ID || b.IdentityID != c.IdentityID {
		return
	}
	if m.checkpointEvents(&b) != nil {
		m.setStatus(c.Platform, "storage_error", "保存回复失败，事件游标未前进，正在重试")
	}
}

func (m *Manager) checkpointEvents(b *binding) error {
	if b.SessionID == "" {
		return nil
	}
	for {
		events, err := m.backend.Events(b.Cursor, b.SessionID, 200)
		if err != nil || len(events) == 0 {
			return err
		}
		checkpoint := *b
		objects := []store.Object{}
		for _, event := range events {
			checkpoint.Cursor = event.Sequence
			if event.DeviceID != b.DeviceID {
				continue
			}
			if text := eventText(event); text != "" {
				objects = append(objects, replyObjects(*b, stable(b.ID, event.ID), text)...)
			}
		}
		objects = append(objects, store.Object{Bucket: "messaging_bindings", ID: b.ID, Value: checkpoint})
		if err := m.store.PutObjects(objects...); err != nil {
			return err
		}
		*b = checkpoint
		if len(events) < 200 {
			return nil
		}
	}
}

// Every chunk has its own durable receipt: retrying a later chunk cannot resend
// an earlier successful one. QQ's passive reply budget also bounds long replies.
func replyObjects(b binding, id, text string) []store.Object {
	limit := 3500
	if b.Envelope.Platform == "feishu" {
		limit = 12000
	}
	chunks := SplitText(text, limit)
	if b.Envelope.Platform == "qq" && len(chunks) > 4 {
		chunks = chunks[:4]
		last := SplitText(chunks[3], 3200)
		chunks[3] = last[0] + "\n\n[回复较长，QQ 仅回传部分内容；完整回复请在 MewDex 对话中查看。]"
	}
	objects := make([]store.Object, 0, len(chunks))
	created := time.Now().UTC()
	for index, chunk := range chunks {
		out := delivery{ID: stable(id, strconv.Itoa(index)), BindingID: b.ID, Envelope: b.Envelope, IdentityID: b.IdentityID, DeviceID: b.DeviceID, ProjectID: b.ProjectID, Text: chunk, State: "pending", CreatedAt: created.Add(time.Duration(index) * time.Nanosecond).Format(time.RFC3339Nano)}
		objects = append(objects, store.Object{Bucket: "messaging_outbox", ID: out.ID, Value: out})
	}
	return objects
}

func (m *Manager) deliver(ctx context.Context) {
	for ctx.Err() == nil {
		pending, err := m.store.PendingObjects("messaging_outbox", 100)
		blocked := map[string]bool{}
		if err == nil {
			for _, raw := range pending {
				var out delivery
				if json.Unmarshal(raw, &out) != nil || blocked[out.BindingID] {
					continue
				}
				if out.NextAt > time.Now().UnixMilli() {
					blocked[out.BindingID] = true
					continue
				}
				c, _ := m.config(out.Envelope.Platform)
				err = m.authorizeDelivery(c, out)
				if errors.Is(err, ErrHubUnavailable) {
					blocked[out.BindingID] = true
					continue
				}
				if err != nil {
					out.State, out.Text, out.Envelope.ReplyToken = "cancelled", "", ""
					_ = m.store.Put("messaging_outbox", out.ID, out)
					continue
				}
				m.mu.Lock()
				run := m.runs[c.Platform]
				m.mu.Unlock()
				if run == nil {
					continue
				}
				sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				err = run.adapter.Send(sendCtx, out.Envelope, out.Text, out.ID)
				cancel()
				if ctx.Err() != nil {
					return
				}
				if err == nil {
					out.State, out.Text, out.Envelope.ReplyToken = "sent", "", ""
				} else {
					out.Attempts++
					out.NextAt = time.Now().Add(min(time.Duration(out.Attempts*out.Attempts)*time.Second, time.Minute)).UnixMilli()
					if out.Attempts >= 8 {
						out.State = "failed"
					}
					blocked[out.BindingID] = true
					m.setStatus(c.Platform, "delivery_error", "回复未送达，正在有界重试；原回复可在 MewDex 中查看")
				}
				if err = m.store.Put("messaging_outbox", out.ID, out); err != nil {
					break
				}
			}
		}
		if !pause(ctx, 300*time.Millisecond) {
			return
		}
	}
}
