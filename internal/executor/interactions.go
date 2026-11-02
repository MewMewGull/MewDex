package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type pendingRequest struct {
	View protocol.Interaction
	RPC  engine.Notification
}

func (x *Executor) recordInteraction(view protocol.Interaction) error {
	if err := x.store.Put("interactions", view.ID, view); err != nil {
		return err
	}
	return x.emit(view.SessionID, "interaction.updated", view)
}

func (x *Executor) receiveRequest(n engine.Notification) {
	reject := func(message string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = x.engine.Reject(ctx, n.RequestID, message)
	}
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(n.Params, &p) != nil || p.ThreadID == "" {
		reject("MewDex does not implement this engine request")
		return
	}
	sessions, err := store.List[protocol.Session](x.store, "sessions")
	if err != nil {
		reject("MewDex request store unavailable")
		return
	}
	for _, s := range sessions {
		if s.EngineID != p.ThreadID {
			continue
		}
		lock := x.lock(s.ID)
		lock.Lock()
		defer lock.Unlock()
		if err = x.store.Get("sessions", s.ID, &s); err != nil || s.EngineID != p.ThreadID || s.Frozen || s.State == "stopping" || s.State == "unknown" {
			reject("MewDex task stopped or its execution state is unconfirmed")
			return
		}
		view, err := engine.RequestView(n, s.ID)
		if err != nil {
			reject(err.Error())
			_ = x.emit(s.ID, "interaction.unsupported", map[string]string{"method": n.Method, "message": err.Error()})
			return
		}
		view.ExecutionScopeID = s.ExecutionScopeID
		if s.ExecutionScopeID == "" {
			reject("MewDex has not confirmed this conversation's execution scope")
			return
		}
		switch n.Method {
		case "item/fileChange/requestApproval":
			preview, reviewErr := x.reviewFileApproval(s, n)
			view.CanAccept = reviewErr == nil
			if reviewErr != nil {
				view.Details = reviewErr.Error()
			} else {
				view.FilePatch = preview.Patch
				view.Details = "仅确认下面这一次修改；当前文件访问限制继续生效。"
			}
		case "item/commandExecution/requestApproval":
			grant, reviewErr := x.reviewCommandApproval(s, n, view.Cwd, view.TurnID)
			view.CanAccept = reviewErr == nil
			view.Permissions = grant
			if reviewErr != nil {
				view.Permissions = nil
				view.Details = reviewErr.Error()
			} else {
				view.Details = "仅确认这一次命令；已有的文件与网络访问限制继续生效。"
			}
		case "item/permissions/requestApproval":
			profile, scopeErr := x.executionScope(s)
			grant, grantErr := engine.RequestedPermissions(n)
			if view.TurnID == "" || view.TurnID != s.TurnID || view.Cwd != profile.Workspace() {
				scopeErr = errors.New("permission request does not match the active turn and workspace")
			}
			if scopeErr == nil && grantErr == nil {
				grant, grantErr = profile.ValidateGrant(grant)
			}
			if scopeErr == nil && grantErr == nil {
				view.Permissions, view.CanAccept = &grant, true
			} else {
				view.CanAccept = false
				view.Permissions = nil
				view.Details = "此请求无法在当前执行范围内授权。可拒绝或停止任务。"
				if grantErr != nil {
					view.Details += "\n" + grantErr.Error()
				}
			}
		}
		if err = x.recordInteraction(view); err != nil {
			reject("MewDex could not persist this request")
			return
		}
		x.mu.Lock()
		x.requests[view.ID] = pendingRequest{View: view, RPC: n}
		x.mu.Unlock()
		return
	}
	reject("MewDex cannot find the conversation for this request")
}

func (x *Executor) pendingInteractions(session string) []protocol.Interaction {
	x.mu.Lock()
	defer x.mu.Unlock()
	views := []protocol.Interaction{}
	for _, request := range x.requests {
		if request.View.SessionID == session {
			views = append(views, request.View)
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt < views[j].CreatedAt })
	return views
}

// Caller holds the session lock. Removing the RPC before writing its response
// prevents another input from replaying a callback after an uncertain write.
func (x *Executor) respond(ctx context.Context, s protocol.Session, c protocol.Command) error {
	x.mu.Lock()
	request, found := x.requests[c.RequestID]
	x.mu.Unlock()
	if !found || request.View.SessionID != s.ID {
		return errors.New("request has expired or was already answered")
	}
	if s.Frozen || (s.State != "running" && !(s.State == "idle" && request.View.TurnID == "")) {
		return errors.New("the task is stopped or its running state is unconfirmed")
	}
	if request.View.TurnID != "" && request.View.TurnID != s.TurnID {
		return errors.New("request belongs to an earlier turn")
	}
	var reply protocol.InteractionReply
	decoder := json.NewDecoder(bytes.NewReader(c.Response))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil {
		return err
	}
	if reply.Action == "accept" {
		if request.View.ExecutionScopeID == "" || request.View.ExecutionScopeID != s.ExecutionScopeID {
			return errors.New("request belongs to another execution scope")
		}
		profile, err := x.executionScope(s)
		if err != nil {
			return err
		}
		if request.RPC.Method == "item/permissions/requestApproval" {
			grant, err := engine.RequestedPermissions(request.RPC)
			if err != nil {
				return err
			}
			grant, err = profile.ValidateGrant(grant)
			if err != nil {
				return err
			}
			if request.View.Permissions == nil || !bytes.Equal(protocol.JSON(grant), protocol.JSON(request.View.Permissions)) {
				return errors.New("权限路径在请求展示后发生变化，请重新发起请求")
			}
		}
		if request.RPC.Method == "item/commandExecution/requestApproval" {
			grant, err := x.reviewCommandApproval(s, request.RPC, request.View.Cwd, request.View.TurnID)
			if err != nil {
				return err
			}
			if !bytes.Equal(protocol.JSON(grant), protocol.JSON(request.View.Permissions)) {
				return errors.New("命令权限路径发生变化，请重新发起请求")
			}
		}
		if request.RPC.Method == "item/fileChange/requestApproval" {
			preview, err := x.reviewFileApproval(s, request.RPC)
			if err != nil {
				return err
			}
			if preview.Patch != request.View.FilePatch {
				return errors.New("文件补丁发生变化，请重新发起请求")
			}
		}
	}
	result, err := engine.ReplyFor(request.RPC, request.View, reply)
	if err != nil {
		return err
	}
	request.View.State = "responding"
	if err = x.recordInteraction(request.View); err != nil {
		return err
	}
	x.mu.Lock()
	delete(x.requests, c.RequestID)
	x.mu.Unlock()
	err = x.engine.Respond(ctx, request.RPC.RequestID, result)
	request.View.State = "answered"
	if err != nil {
		request.View.State = "unknown"
	}
	if saveErr := x.recordInteraction(request.View); saveErr != nil && err == nil {
		err = &engine.UnknownOutcome{Cause: saveErr}
	}
	if err != nil && !engine.IsUnknownOutcome(err) {
		return &engine.UnknownOutcome{Cause: err}
	}
	return err
}

func (x *Executor) reviewCommandApproval(s protocol.Session, n engine.Notification, cwd, turn string) (*protocol.PermissionGrant, error) {
	profile, err := x.executionScope(s)
	if err != nil {
		return nil, err
	}
	if turn == "" || turn != s.TurnID || !profile.HasDeniedReads() {
		return nil, errors.New("命令请求与当前受限回合不一致")
	}
	if err = profile.ValidateCommandDirectory(cwd); err != nil {
		return nil, err
	}
	grant, err := engine.CommandPermissions(n)
	if err != nil || grant == nil {
		return grant, err
	}
	normalized, err := profile.ValidateCommandGrant(*grant)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (x *Executor) reviewFileApproval(s protocol.Session, n engine.Notification) (engine.FileChangePreview, error) {
	profile, err := x.executionScope(s)
	if err != nil {
		return engine.FileChangePreview{}, err
	}
	var request struct {
		TurnID    string  `json:"turnId"`
		ItemID    string  `json:"itemId"`
		GrantRoot *string `json:"grantRoot"`
	}
	if json.Unmarshal(n.Params, &request) != nil || request.TurnID == "" || request.TurnID != s.TurnID || request.ItemID == "" || !profile.HasDeniedReads() {
		return engine.FileChangePreview{}, errors.New("文件请求与当前受限回合不一致")
	}
	if request.GrantRoot != nil && *request.GrantRoot != "" {
		return engine.FileChangePreview{}, errors.New("此请求会持续开放目录，请改用仅本回合的额外权限请求")
	}
	x.mu.Lock()
	entry, ok := x.filePreviews[filePreviewKey(s.EngineID, s.TurnID, request.ItemID)]
	x.mu.Unlock()
	if !ok || entry.SessionID != s.ID || entry.ScopeID != s.ExecutionScopeID {
		return engine.FileChangePreview{}, errors.New("引擎尚未提供与本回合匹配的文件变更")
	}
	if entry.Error != "" {
		return engine.FileChangePreview{}, errors.New(entry.Error)
	}
	return entry.Preview, nil
}

func (x *Executor) expireInteractions(session, turn string, rpcID json.RawMessage) error {
	x.mu.Lock()
	expired := []protocol.Interaction{}
	if len(rpcID) == 0 {
		for key, entry := range x.filePreviews {
			if (session == "" || entry.SessionID == session) && (turn == "" || entry.Preview.TurnID == turn) {
				delete(x.filePreviews, key)
			}
		}
	}
	for id, request := range x.requests {
		if session != "" && request.View.SessionID != session || turn != "" && request.View.TurnID != turn || len(rpcID) > 0 && !bytes.Equal(rpcID, request.RPC.RequestID) {
			continue
		}
		request.View.State = "expired"
		expired = append(expired, request.View)
		delete(x.requests, id)
	}
	x.mu.Unlock()
	for _, view := range expired {
		if err := x.recordInteraction(view); err != nil {
			return err
		}
	}
	return nil
}
