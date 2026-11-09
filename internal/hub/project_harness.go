package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (h *Hub) projectHarnessAPI(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 2 || parts[0] == "" || parts[1] != "harness" {
		problem(w, 404, store.ErrNotFound)
		return
	}
	projectID := parts[0]
	id := identity(r)
	if !id.Access.Allows(h.coordinatorID, projectID) {
		problem(w, 403, errors.New("此输入端没有该项目的访问权限"))
		return
	}
	if len(parts) == 3 && parts[2] == "turn" && r.Method == "GET" {
		var session protocol.Session
		if err := h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session); err != nil || session.DeviceID != h.coordinatorID || session.ProjectID != projectID {
			problem(w, 404, store.ErrNotFound)
			return
		}
		var value protocol.ProjectTurnContext
		if err := h.store.Get("project_turns", session.ID+":"+session.LastStartID, &value); err != nil {
			problem(w, 404, err)
			return
		}
		reply(w, 200, value)
		return
	}
	if len(parts) == 3 && parts[2] == "history" && r.Method == "GET" {
		before, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		if r.URL.Query().Get("before") == "" {
			before, err = 0, nil
		}
		if err != nil || before < 0 {
			problem(w, 400, errors.New("invalid project configuration cursor"))
			return
		}
		if _, err = h.store.ProjectHarness(projectID, 0); err != nil {
			problem(w, 404, err)
			return
		}
		page, err := h.store.ProjectHarnessHistory(projectID, before)
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, page)
		return
	}
	if len(parts) != 2 {
		problem(w, 404, store.ErrNotFound)
		return
	}
	if r.Method == "GET" {
		revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if r.URL.Query().Get("revision") == "" {
			revision, err = 0, nil
		}
		if err != nil || revision < 0 {
			problem(w, 400, errors.New("invalid project configuration revision"))
			return
		}
		value, err := h.store.ProjectHarness(projectID, revision)
		if err != nil {
			problem(w, 404, err)
			return
		}
		reply(w, 200, value)
		return
	}
	if r.Method != "PUT" || id.Role != "admin" {
		problem(w, 403, errors.New("只有管理员可以编辑项目指令和自动化配置"))
		return
	}
	var input protocol.ProjectHarnessUpdate
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		problem(w, 400, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		problem(w, 400, errors.New("expected one project configuration request"))
		return
	}
	h.projectMu.Lock()
	value, err := h.store.SaveProjectHarness(h.config.DeviceID, projectID, input)
	h.projectMu.Unlock()
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrConflict) {
			status = 409
			err = errors.New("项目或配置已在其他输入端修改，请刷新后重试")
		}
		problem(w, status, err)
		return
	}
	h.notify()
	reply(w, 200, value)
}

func (e *coordinatorEngine) prepareProjectTurn(session protocol.Session) (protocol.ProjectTurnContext, error) {
	var value protocol.ProjectTurnContext
	if session.LastStartID == "" {
		return value, errors.New("project turn requires its original command identity")
	}
	key := session.ID + ":" + session.LastStartID
	h := e.coordinator.hub
	h.projectMu.Lock()
	defer h.projectMu.Unlock()
	if err := h.store.Get("project_turns", key, &value); err == nil {
		if value.SessionID != session.ID || value.CommandID != session.LastStartID || value.Project.ID != session.ProjectID {
			return value, errors.New("project context belongs to another turn")
		}
		return value, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return value, err
	}
	if err := h.store.Get("hub_projects", session.ProjectID, &value.Project); err != nil {
		return value, err
	}
	configuration, err := h.store.ProjectHarness(session.ProjectID, 0)
	if err != nil {
		return value, err
	}
	value.Harness = configuration
	instructions, err := h.projectInstructions(value.Project)
	if err != nil {
		return value, err
	}
	value.SessionID, value.CommandID = session.ID, session.LastStartID
	value.Instructions, value.CreatedAt = appendProjectConfiguration(instructions, configuration), protocol.Now()
	inserted, err := h.store.InsertObject("project_turns", key, value)
	if err != nil {
		return value, err
	}
	if !inserted {
		err = h.store.Get("project_turns", key, &value)
	}
	return value, err
}

func appendProjectConfiguration(instructions string, configuration protocol.ProjectHarnessState) string {
	if configuration.Revision == 0 {
		return instructions
	}
	effective := configuration.Configuration
	if effective.Memory != nil && !effective.Memory.Enabled {
		effective.Memory = nil
	}
	return instructions + "\n\nMewDex project configuration format 1, revision " + strconv.FormatInt(configuration.Revision, 10) + ", content version " + configuration.Version + ". Apply the project instructions below. Skills are reusable project guidance. Enabled memory contains user-maintained project context. Verify changeable memory against current evidence. Scripts have fixed argv and durable machine/directory bindings. Workflow steps reference script IDs in order. Use the existing mewdex execution tools with the matching directory index, retain the current turn's access mode, and wait for each step's confirmed success before continuing. Stop a workflow on failure or an unknown result.\n" + string(protocol.JSON(effective))
}

// The pinned engine's model catalogue can replace collaboration-mode text.
// Application context is a separate developer-message input. Its renderer limits
// each fragment to 1,000 tokens, so byte-bounded parts retain complete Unicode
// instructions and argv even when a configuration exceeds one fragment.
func projectApplicationContext(value protocol.ProjectTurnContext, task string) map[string]any {
	document := "mewdex_project_" + store.Hash(value.CommandID)[:12]
	text := "Apply the current MewDex project document " + document + ". Read all its numbered parts in order as one document. This document replaces earlier MewDex project documents.\n\n" + value.Instructions + "\n\n" + task
	parts := []string{}
	for len(text) > 0 {
		end := min(768, len(text))
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		parts = append(parts, text[:end])
		text = text[end:]
	}
	fragments := make(map[string]any, len(parts))
	for index, part := range parts {
		fragments[fmt.Sprintf("%s_%04d", document, index)] = map[string]string{"kind": "application", "value": fmt.Sprintf("MewDex project document part %d/%d\n", index+1, len(parts)) + part}
	}
	return fragments
}

func (c *Coordinator) addProjectConfigurationSource(command protocol.Command, result *protocol.CommandResult) error {
	var inspection protocol.PromptInspection
	if err := json.Unmarshal(result.Data, &inspection); err != nil {
		return err
	}
	var previous protocol.ProjectTurnContext
	if command.Policy == nil && result.Session != nil && result.Session.LastStartID != "" {
		err := c.hub.store.Get("project_turns", result.Session.ID+":"+result.Session.LastStartID, &previous)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	configuration, text, note := previous.Harness, previous.Instructions, "来自该回合已固定的项目配置快照"
	if text == "" {
		c.hub.projectMu.Lock()
		defer c.hub.projectMu.Unlock()
		var err error
		configuration, err = c.hub.store.ProjectHarness(inspection.ProjectID, 0)
		if err != nil {
			return err
		}
		var project protocol.Project
		if err = c.hub.store.Get("hub_projects", inspection.ProjectID, &project); err != nil {
			return err
		}
		text, err = c.hub.projectInstructions(project)
		if err != nil {
			return err
		}
		text = appendProjectConfiguration(text, configuration)
		note = "当前项目配置会在下一回合开始时固定，排队输入在实际启动时读取"
	}
	inspection.Sources = append(inspection.Sources, protocol.PromptSource{ID: "hub-project:" + configuration.Version, Kind: "project-configuration", Label: "Hub 项目配置 · v" + strconv.FormatInt(configuration.Revision, 10), Text: text, Note: note, ReadOnly: true, Enabled: true})
	result.Data = protocol.JSON(inspection)
	return nil
}
