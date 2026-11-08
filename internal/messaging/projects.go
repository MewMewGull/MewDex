package messaging

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func projectLabel(project protocol.Project) string {
	if project.Name != "" {
		return project.Name
	}
	return project.ID
}

func (m *Manager) availableProjects(c channel, e Envelope) ([]protocol.Project, error) {
	identity, err := m.authorize(c, e)
	if err != nil {
		return nil, err
	}
	var device protocol.Device
	if err := m.backend.Get("devices", c.DeviceID, &device); err != nil {
		return nil, err
	}
	projects := []protocol.Project{}
	for _, project := range device.Projects {
		if !project.Removed && project.ID != "" && (c.ProjectID == "" || c.ProjectID == project.ID) && identity.Access.Allows(device.ID, project.ID) {
			projects = append(projects, project)
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		if projectLabel(projects[i]) == projectLabel(projects[j]) {
			return projects[i].ID < projects[j].ID
		}
		return projectLabel(projects[i]) < projectLabel(projects[j])
	})
	return projects, nil
}

func projectList(b *binding, projects []protocol.Project) string {
	b.ProjectChoices = []string{}
	if len(projects) == 0 {
		return "暂无可用项目，请先在 MewDex 中创建项目并检查访问权限。"
	}
	var text strings.Builder
	text.WriteString("可选项目")
	for index, project := range projects {
		b.ProjectChoices = append(b.ProjectChoices, project.ID)
		fmt.Fprintf(&text, "\n%d. %s [%s]", index+1, projectLabel(project), project.ID)
		if project.ID == b.ProjectID {
			text.WriteString("（当前项目）")
		}
	}
	text.WriteString("\n发送 /project 序号、项目名或项目 ID 选择，例如 /project 1。切换项目后，下条任务会创建新会话。")
	return text.String()
}

func (m *Manager) listProjects(c channel, b *binding) (string, error) {
	projects, err := m.availableProjects(c, b.Envelope)
	if err != nil {
		return "", err
	}
	return projectList(b, projects), nil
}

func (m *Manager) selectProject(c channel, b *binding, query string) (string, error) {
	projects, err := m.availableProjects(c, b.Envelope)
	if err != nil {
		return "", err
	}
	// Numbers refer to the last list shown in this chat, even if the Hub's
	// catalogue changed meanwhile. IDs and names are checked against live access.
	id := query
	number, numberErr := strconv.Atoi(query)
	if numberErr == nil {
		if number < 1 || number > len(b.ProjectChoices) {
			return "请按最新项目列表选择。\n" + projectList(b, projects), nil
		}
		id = b.ProjectChoices[number-1]
	}
	var selected *protocol.Project
	for index := range projects {
		if projects[index].ID == id {
			selected = &projects[index]
			break
		}
	}
	if selected == nil && numberErr != nil {
		for index := range projects {
			if projects[index].Name == query {
				if selected != nil {
					return "有多个同名项目，请使用序号或项目 ID。\n" + projectList(b, projects), nil
				}
				selected = &projects[index]
			}
		}
	}
	if selected == nil {
		return "项目不存在或无权访问，请重新选择。\n" + projectList(b, projects), nil
	}
	if b.DeviceID == c.DeviceID && b.ProjectID == selected.ID {
		return "当前项目为 " + projectLabel(*selected) + " [" + selected.ID + "]。发送文字即可继续。", nil
	}
	if b.SessionID != "" {
		var session protocol.Session
		if err := m.backend.Get("sessions", b.SessionID, &session); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if reason, err := m.resetBlocked(session); err != nil || reason != "" {
			return reason, err
		}
		if _, err := m.authorizeBinding(c, *b); err == nil {
			if err := m.checkpointEvents(b); err != nil {
				return "", err
			}
		}
	}
	b.DeviceID, b.ProjectID = c.DeviceID, selected.ID
	b.SessionID, b.CreateID, b.Cursor = "", "", 0
	return "已选择项目 " + projectLabel(*selected) + " [" + selected.ID + "]。发送文字即可开始新会话。", nil
}

func (m *Manager) resetBlocked(session protocol.Session) (string, error) {
	if session.ExecutionUnconfirmed() || session.Frozen || session.ExecutionRecovery != nil {
		return "当前会话还有执行或队列状态需要处理，请先用 /stop 停止任务，或在 MewDex 中确认后再新建会话或切换项目。", nil
	}
	if session.ID == "" {
		return "", nil
	}
	items, err := m.backend.QueueItems(session.ID)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.State == "queued" || item.State == "dispatching" || item.State == "running" || item.State == "unknown" {
			return "当前会话还有未完成的队列消息，请先在 MewDex 中处理后再新建会话或切换项目。", nil
		}
	}
	return "", nil
}

func (m *Manager) authorizeBinding(c channel, b binding) (protocol.Identity, error) {
	identity, err := m.authorize(c, b.Envelope)
	if err != nil {
		return identity, err
	}
	if b.DeviceID != c.DeviceID || b.ProjectID == "" || c.ProjectID != "" && b.ProjectID != c.ProjectID || !identity.Access.Allows(b.DeviceID, b.ProjectID) {
		return identity, errors.New("当前项目访问权限已撤销，请使用 /projects 查看并重新选择项目")
	}
	return identity, nil
}
