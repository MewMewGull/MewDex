package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type computerTarget struct {
	HubProjectID string                    `json:"hubProjectId"`
	DeviceID     string                    `json:"deviceId"`
	ProjectID    string                    `json:"projectId"`
	OwnerID      string                    `json:"ownerId"`
	SessionID    string                    `json:"sessionId"`
	Session      *protocol.ComputerSession `json:"session,omitempty"`
}

func computerTargetKey(owner, device string) string {
	return store.Hash(string(protocol.JSON([]string{owner, device})))
}

func (h *Hub) computerStatus(owner, device string) (protocol.ComputerStatus, error) {
	var node protocol.Device
	if err := h.store.Get("devices", device, &node); err != nil {
		return protocol.ComputerStatus{}, err
	}
	status := protocol.ComputerStatus{Supported: slices.Contains(node.Capabilities, "computer-use-v1")}
	if !status.Supported {
		status.Reason = "该执行端尚未提供原生 Computer Use 驱动"
	}
	h.mu.Lock()
	online := h.peers[device] != nil || device == h.config.DeviceID && h.local != nil
	h.mu.Unlock()
	if !online {
		status.Reason = "执行端离线，窗口控制状态尚未确认"
	}
	var target computerTarget
	if h.store.Get("computer_targets", computerTargetKey(owner, device), &target) == nil && target.OwnerID == owner && target.DeviceID == device {
		status.Session = target.Session
		if status.Session != nil && (status.Session.State == "pending" || status.Session.State == "approved") {
			deadline, err := time.Parse(time.RFC3339Nano, status.Session.ExpiresAt)
			if err == nil && !time.Now().Before(deadline) {
				status.Session.State = "expired"
			}
		}
	}
	return status, nil
}

func (h *Hub) recordComputerSession(device string, session protocol.ComputerSession) error {
	key := computerTargetKey(session.OwnerID, device)
	h.terminalMu.Lock()
	defer h.terminalMu.Unlock()
	var target computerTarget
	if err := h.store.Get("computer_targets", key, &target); errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if target.DeviceID != device || session.DeviceID != device || target.OwnerID != session.OwnerID || target.ProjectID != session.ProjectID || target.SessionID != session.ID {
		return nil
	}
	if target.Session != nil && target.Session.Revision >= session.Revision {
		return nil
	}
	target.Session = &session
	return h.store.Put("computer_targets", key, target)
}

func (e *coordinatorEngine) executeComputerTool(ctx context.Context, request remoteToolCall, session protocol.Session, directory protocol.ProjectDirectory) (json.RawMessage, error) {
	operation := request.Arguments.Operation
	if !slices.Contains([]string{"request", "status", "observe", "act", "stop"}, operation) {
		return nil, errors.New("unsupported computer operation")
	}
	if operation == "act" && (session.Policy.AccessMode == "read-only" || session.Policy.CollaborationMode == "plan") {
		return nil, errors.New("当前回合为只读，窗口输入需要先调整任务权限并获得本机窗口授权")
	}
	hash := sha256.Sum256(protocol.JSON([]string{request.ThreadID, request.TurnID, request.CallID}))
	command := protocol.Command{ID: "cu_" + hex.EncodeToString(hash[:]), DeviceID: directory.DeviceID, ProjectID: directory.ProjectID, Kind: "computer." + operation, SourceID: "hub", Computer: &protocol.ComputerRequest{OwnerID: session.ID, TurnID: request.TurnID, SessionID: request.Arguments.SessionID, Reason: request.Arguments.Reason, ObservationID: request.Arguments.ObservationID, Action: request.Arguments.Action}}
	return e.coordinator.hub.runComputer(ctx, command, session.ProjectID)
}

func (h *Hub) runComputer(ctx context.Context, command protocol.Command, hubProjectID string) (json.RawMessage, error) {
	execution := h.executionTarget(command)
	execution.AccessMode = "window-grant"
	var session protocol.Session
	sessionErr := h.store.Get("sessions", command.Computer.OwnerID, &session)
	if sessionErr == nil && session.TurnID == command.Computer.TurnID && session.LastStartID != "" {
		var captured protocol.ProjectTurnContext
		if h.store.Get("project_turns", session.ID+":"+session.LastStartID, &captured) == nil {
			execution.HarnessVersion, execution.HarnessRevision = captured.Harness.Version, captured.Harness.Revision
		}
	}
	data, callErr := h.runComputerCommand(ctx, command, hubProjectID)
	var result protocol.ComputerResult
	if callErr == nil && json.Unmarshal(data, &result) == nil {
		result.ExecutionTarget = execution
		data = protocol.JSON(result)
	}
	if sessionErr == nil && session.DeviceID == h.coordinatorID {
		status, message := "completed", ""
		if callErr != nil {
			status, message = "failed", callErr.Error()
			if receipt, err := h.store.Result(command.ID); err == nil && receipt.State == "unknown" {
				status = "unknown"
			}
		}
		window := ""
		if result.Status.Session != nil && result.Status.Session.Window != nil {
			window = result.Status.Session.Window.Title
		}
		_, eventErr := h.store.Ingest(protocol.Event{ID: command.ID + ".computer", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "engine.item/completed", CreatedAt: protocol.Now(), Payload: protocol.JSON(map[string]any{"threadId": session.EngineID, "turnId": command.Computer.TurnID, "item": map[string]any{"id": command.ID, "type": "computerUse", "operation": strings.TrimPrefix(command.Kind, "computer."), "window": window, "status": status, "error": message, "executionTarget": execution}})})
		if eventErr != nil {
			return nil, errors.New("窗口指令结果的历史保存尚未确认，请核对原指令")
		}
		h.notify()
	}
	return data, callErr
}

func (h *Hub) runComputerCommand(ctx context.Context, command protocol.Command, hubProjectID string) (json.RawMessage, error) {
	request := command.Computer
	status, err := h.computerStatus(request.OwnerID, command.DeviceID)
	if err != nil {
		return nil, err
	}
	if !status.Supported {
		return nil, errors.New(status.Reason)
	}
	h.terminalMu.Lock()
	if command.Kind == "computer.request" || command.Kind == "computer.act" || command.Kind == "computer.observe" {
		var stopped bool
		err = h.store.Get("terminal_stopped_owners", request.OwnerID+":"+request.TurnID, &stopped)
		if errors.Is(err, store.ErrNotFound) {
			err = nil
		}
		if err == nil && stopped {
			err = errors.New("此回合的窗口控制已停止")
		}
	}
	if err == nil && command.Kind == "computer.request" {
		if status.Session != nil && status.Session.State != "stopped" && status.Session.State != "expired" {
			h.terminalMu.Unlock()
			return protocol.JSON(protocol.ComputerResult{Status: status}), nil
		}
		target := computerTarget{HubProjectID: hubProjectID, DeviceID: command.DeviceID, ProjectID: command.ProjectID, OwnerID: request.OwnerID, SessionID: command.ID}
		err = h.store.Put("computer_targets", computerTargetKey(request.OwnerID, command.DeviceID), target)
	}
	h.terminalMu.Unlock()
	if err != nil {
		return nil, err
	}
	result, err := h.runTerminal(ctx, command)
	if err != nil {
		return nil, err
	}
	var reply protocol.ComputerResult
	if json.Unmarshal(result.Data, &reply) != nil {
		return nil, errors.New("invalid computer control response")
	}
	if reply.Status.Session != nil {
		if err = h.recordComputerSession(command.DeviceID, *reply.Status.Session); err != nil {
			return nil, errors.New("窗口结果已返回但 Hub 状态尚未确认，请查询原会话")
		}
	}
	return result.Data, nil
}

func computerToolResponse(tool string, data json.RawMessage, callErr error) json.RawMessage {
	text := string(data)
	items := []map[string]string{}
	if callErr == nil && tool == "mewdex_computer" {
		var result protocol.ComputerResult
		if json.Unmarshal(data, &result) == nil && result.Observation != nil {
			observation := result.Observation
			if observation.MimeType == "image/jpeg" && observation.Image != "" && len(observation.Image) <= 1500000 {
				items = append(items, map[string]string{"type": "inputImage", "imageUrl": "data:image/jpeg;base64," + observation.Image})
			}
			observation.Image = ""
			text = string(protocol.JSON(result))
		}
	}
	if callErr != nil {
		text = callErr.Error()
	}
	items = append([]map[string]string{{"type": "inputText", "text": text}}, items...)
	return protocol.JSON(map[string]any{"success": callErr == nil, "contentItems": items})
}

func (h *Hub) stopComputerOwner(ctx context.Context, owner string) error {
	conversation, turn, _ := strings.Cut(owner, ":")
	if turn == "" {
		var session protocol.Session
		if h.store.Get("sessions", conversation, &session) == nil {
			turn = session.TurnID
		}
	}
	targets, err := store.List[computerTarget](h.store, "computer_targets")
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.OwnerID != conversation || target.Session != nil && (target.Session.State == "stopped" || target.Session.State == "expired") {
			continue
		}
		command := protocol.Command{ID: "cu_stop_" + protocol.ID(), DeviceID: target.DeviceID, ProjectID: target.ProjectID, Kind: "computer.stop", SourceID: "hub", Computer: &protocol.ComputerRequest{OwnerID: conversation, TurnID: turn, SessionID: target.SessionID}}
		if _, stopErr := h.runComputer(ctx, command, target.HubProjectID); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
	}
	return err
}

func (h *Hub) computerAPI(w http.ResponseWriter, r *http.Request, projectID string) {
	if !identity(r).Access.Allows(h.coordinatorID, projectID) {
		problem(w, 403, errors.New("此输入端没有该项目的访问权限"))
		return
	}
	var session protocol.Session
	var project protocol.Project
	index, err := strconv.Atoi(r.URL.Query().Get("directory"))
	if err != nil || h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || session.DeviceID != h.coordinatorID || session.ProjectID != projectID || h.store.Get("hub_projects", projectID, &project) != nil || index < 0 || index >= len(project.Directories) {
		problem(w, 400, errors.New("请选择当前项目的对话和执行目录"))
		return
	}
	directory := project.Directories[index]
	if r.Method == "GET" {
		status, err := h.computerStatus(session.ID, directory.DeviceID)
		if err != nil {
			problem(w, 400, err)
			return
		}
		reply(w, 200, status)
		return
	}
	if r.Method != "POST" {
		problem(w, 405, errors.New("unsupported computer management operation"))
		return
	}
	var input protocol.ComputerStopRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.ID == "" || len(input.ID) > 128 || input.SessionID == "" {
		problem(w, 400, errors.New("invalid computer stop request"))
		return
	}
	data, err := h.runComputer(r.Context(), protocol.Command{ID: input.ID, DeviceID: directory.DeviceID, ProjectID: directory.ProjectID, Kind: "computer.stop", SourceID: "hub", Computer: &protocol.ComputerRequest{OwnerID: session.ID, TurnID: session.TurnID, SessionID: input.SessionID}}, projectID)
	if err != nil {
		problem(w, 409, err)
		return
	}
	var result protocol.ComputerResult
	_ = json.Unmarshal(data, &result)
	reply(w, 200, result.Status)
}
