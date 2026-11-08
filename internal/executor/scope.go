package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"mewdex/internal/attachments"
	"mewdex/internal/engine"
	"mewdex/internal/executionscope"
	"mewdex/internal/protocol"
	"mewdex/internal/workspace"
)

func scopeParams(profile executionscope.Profile) map[string]any {
	return map[string]any{"cwd": profile.Workspace(), "permissions": profile.ID(), "config": profile.ThreadConfig(), "approvalPolicy": "on-request", "approvalsReviewer": "user"}
}

type scopedThread struct {
	Model         string `json:"model"`
	ModelProvider string `json:"modelProvider"`
	Thread        struct {
		ID string `json:"id"`
	} `json:"thread"`
}

func (x *Executor) callScopedThread(ctx context.Context, method string, params map[string]any, profile executionscope.Profile) (scopedThread, error) {
	var reply scopedThread
	var raw json.RawMessage
	if err := x.engine.Call(ctx, method, params, &raw); err != nil {
		return reply, err
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return reply, &engine.UnknownOutcome{Cause: err}
	}
	if err := profile.VerifyThread(raw); err != nil {
		return reply, &engine.UnknownOutcome{Cause: err}
	}
	if reply.Thread.ID == "" {
		return reply, &engine.UnknownOutcome{Cause: errors.New("engine did not return the scoped conversation identifier")}
	}
	return reply, nil
}

func (x *Executor) sessionView(s protocol.Session, private []string) (*attachments.View, error) {
	if s.ID == "" || s.ProjectID == "" || s.DeviceID != x.config.DeviceID {
		return nil, errors.New("input view requires this executor's session")
	}
	x.scopeMu.Lock()
	defer x.scopeMu.Unlock()
	if view := x.views[s.ID]; view != nil {
		return view, nil
	}
	parent := x.inputParents[s.ProjectID]
	if parent == nil {
		data, err := workspace.CanonicalDir(x.config.DataDir)
		if err != nil {
			return nil, err
		}
		base, err := executionscope.PublicInputParent(filepath.Dir(data), private)
		if err != nil {
			return nil, err
		}
		target := attachments.ViewTarget{DeviceID: x.config.DeviceID, ProjectID: "local-input-views:" + s.ProjectID, SessionID: data}
		parent, err = attachments.NewView(filepath.Join(base, "mewdex-inputs-"+target.Key()), target)
		if err != nil {
			return nil, err
		}
		x.inputParents[s.ProjectID] = parent
	}
	target := attachments.ViewTarget{DeviceID: s.DeviceID, ProjectID: s.ProjectID, SessionID: s.ID}
	view, err := attachments.NewView(filepath.Join(parent.Directory(), target.Key()), target)
	if err != nil {
		return nil, err
	}
	x.views[s.ID] = view
	return view, nil
}

func (x *Executor) executionScope(s protocol.Session) (executionscope.Profile, error) {
	var empty executionscope.Profile
	project, err := x.project(s.ProjectID)
	if err != nil {
		return empty, err
	}
	root, err := workspace.CanonicalDir(project.Path)
	if err != nil || root != x.projectRoot(project.ID) {
		return empty, errors.New("execution project changed; review the local configuration")
	}
	environment, err := x.engine.ExecutionEnvironment()
	if err != nil {
		return empty, err
	}
	private := append([]string{x.config.DataDir}, environment.Private...)
	private = append(private, x.config.PrivatePaths...)
	if x.config.ProfilePath != "" {
		path, err := executionscope.PrivateProfilePath(root, x.config.ProfilePath)
		if err != nil {
			return empty, err
		}
		private = append(private, path)
	}
	for _, path := range []string{x.config.NodeTokenFile, x.config.Engine.ProxyTokenFile} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return empty, errors.New("a configured private execution file is missing or unavailable")
		}
		private = append(private, path)
	}
	view, err := x.sessionView(s, private)
	if err != nil {
		return empty, err
	}
	readOnly := append([]string{view.Directory()}, environment.RuntimeReadOnly...)
	accessMode := s.Policy.AccessMode
	if s.EngineStarted && (s.State == "running" || s.State == "stopping") {
		accessMode = s.ScopeAccessMode
	}
	profile, err := executionscope.Build(executionscope.Spec{DeviceID: s.DeviceID, ProjectID: s.ProjectID, EngineVersion: engine.CompatibleVersion, Workspace: root, ReadOnly: readOnly, Private: private, AccessMode: accessMode})
	if err != nil {
		return empty, err
	}
	if s.ExecutionScopeID != "" && s.ExecutionScopeID != profile.ID() && s.ScopeAccessMode == accessMode {
		return empty, errors.New("execution permissions or environment changed; review the local configuration before continuing this conversation")
	}
	return profile, nil
}
