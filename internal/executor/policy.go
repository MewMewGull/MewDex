package executor

import (
	"context"
	"encoding/json"
	"errors"

	"mewdex/internal/engine"
	"mewdex/internal/executionscope"
	"mewdex/internal/policy"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func setPolicy(s *protocol.Session, p protocol.Policy) {
	s.Policy = p
	s.PolicyInstructions = policy.Instructions(p)
	s.PolicyVersion = policy.VersionFor(p, s.PolicyInstructions)
}

func (x *Executor) snapshot(s protocol.Session) error {
	id := s.ID + ":" + s.PolicyVersion
	var prior protocol.PolicySnapshot
	if err := x.store.Get("policies", id, &prior); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	value := protocol.PolicySnapshot{ID: id, SessionID: s.ID, Version: s.PolicyVersion, Policy: s.Policy, DeveloperInstructions: s.PolicyInstructions, CreatedAt: protocol.Now()}
	if err := x.store.Put("policies", id, value); err != nil {
		return err
	}
	return x.emit(s.ID, "policy.snapshot", value)
}

func (x *Executor) threadParams(p protocol.Policy, profile executionscope.Profile) map[string]any {
	params := scopeParams(profile)
	if p.Model != "" {
		params["model"] = p.Model
	}
	if p.BaseInstructions != nil {
		params["baseInstructions"] = *p.BaseInstructions
	}
	return params
}

func (x *Executor) inspectPolicy(ctx context.Context, projectID string, s *protocol.Session, draft *protocol.Policy) (protocol.PromptInspection, error) {
	project, err := x.project(projectID)
	if err != nil {
		return protocol.PromptInspection{}, err
	}
	var p protocol.Policy
	if s != nil {
		p = s.Policy
	}
	if draft != nil {
		p = *draft
	}
	p, err = policy.Normalize(p)
	if err != nil {
		return protocol.PromptInspection{}, err
	}
	inspection := protocol.PromptInspection{ProjectID: projectID, Policy: p, Version: policy.Version(p), DeveloperInstructions: policy.Instructions(p), Sources: policy.ProjectSources(project.Path), Notes: []string{"子目录的项目指令可随任务访问目录变化；此处展示当前项目根目录与引擎配置来源。"}}
	if s != nil && string(protocol.JSON(p)) == string(protocol.JSON(s.Policy)) {
		inspection.Version = s.PolicyVersion
		inspection.DeveloperInstructions = s.PolicyInstructions
	}
	var config json.RawMessage
	if err = x.engine.Call(ctx, "config/read", map[string]any{"cwd": project.Path, "includeLayers": true}, &config); err != nil {
		inspection.Notes = append(inspection.Notes, "暂时无法读取引擎配置来源")
	} else if sources, e := policy.ConfigSources(config); e == nil {
		inspection.Sources = append(inspection.Sources, sources...)
	}
	base := protocol.PromptSource{ID: "engine-base", Kind: "engine-base", Label: "会话初始基础指令", ReadOnly: true, Enabled: true, Note: "首回合持久化后可读取执行端快照"}
	if s != nil {
		var text string
		if x.store.Get("base_defaults", s.ID, &text) == nil {
			base.Text = text
			base.Note = "保留会话初始版本；继承基础指令时使用此快照"
		} else if s.EngineStarted {
			if reader, ok := x.engine.(engine.BaseInstructionReader); ok {
				if text, e := reader.ReadBaseInstructions(ctx, s.EngineID); e == nil {
					base.Text = text
					base.Note = "来自当前引擎会话的持久化快照"
				}
			}
		}
	}
	inspection.Sources = append(inspection.Sources, base, protocol.PromptSource{ID: "mewdex-task", Kind: "task", Label: "本回合任务指令", Text: inspection.DeveloperInstructions, Enabled: true})
	if s != nil {
		x.scopeMu.Lock()
		attachmentContext := x.attachmentContexts[s.ID]
		x.scopeMu.Unlock()
		if attachmentContext.Footer != "" {
			inspection.Sources = append(inspection.Sources, protocol.PromptSource{ID: "mewdex-attachment-paths", Kind: "runtime-context", Label: "历史附件路径", Text: attachmentContext.Footer, ReadOnly: true, Enabled: true})
		}
	}
	return inspection, nil
}

func (x *Executor) editPolicy(s *protocol.Session, c protocol.Command) error {
	if c.ExpectedPolicyVersion == "" || c.ExpectedPolicyVersion != s.PolicyVersion {
		return errors.New("policy changed on another input; refresh before saving")
	}
	if s.State != "idle" && s.State != "paused" && s.State != "failed" {
		return errors.New("edit the next-turn policy after the active task finishes")
	}
	if c.Kind == "policy.restore" {
		var snapshot protocol.PolicySnapshot
		if err := x.store.Get("policies", s.ID+":"+c.RequestID, &snapshot); err != nil {
			return err
		}
		if snapshot.SessionID != s.ID || snapshot.Version != c.RequestID {
			return errors.New("policy snapshot belongs to another session")
		}
		s.Policy = snapshot.Policy
		s.PolicyInstructions = snapshot.DeveloperInstructions
		s.PolicyVersion = snapshot.Version
	} else {
		if c.Policy == nil {
			return errors.New("policy required")
		}
		p, err := policy.Normalize(*c.Policy)
		if err != nil {
			return err
		}
		setPolicy(s, p)
	}
	if err := x.snapshot(*s); err != nil {
		return err
	}
	return x.save(s)
}

func (x *Executor) reconfigureBase(ctx context.Context, s *protocol.Session) error {
	if s.BaseVersion == policy.BaseVersion(s.Policy) {
		return nil
	}
	if !s.EngineStarted {
		if err := x.replaceUnstarted(ctx, s); err != nil {
			return err
		}
		s.BaseVersion = policy.BaseVersion(s.Policy)
		return nil
	}
	if len(x.pendingInteractions(s.ID)) > 0 {
		return errors.New("answer or cancel outstanding requests before changing base instructions")
	}
	running, err := x.backgroundRunning(ctx, s.EngineID)
	if err != nil {
		return err
	}
	if running {
		return errors.New("stop this conversation's background processes before changing base instructions")
	}
	var baseline string
	if err := x.store.Get("base_defaults", s.ID, &baseline); errors.Is(err, store.ErrNotFound) {
		reader, ok := x.engine.(engine.BaseInstructionReader)
		if !ok {
			return errors.New("this engine cannot capture its initial base instructions")
		}
		baseline, err = reader.ReadBaseInstructions(ctx, s.EngineID)
		if err != nil {
			return err
		}
		if err = x.store.Put("base_defaults", s.ID, baseline); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	profile, err := x.executionScope(*s)
	if err != nil {
		return err
	}
	params := x.threadParams(s.Policy, profile)
	params["threadId"] = s.EngineID
	params["excludeTurns"] = true
	if s.Policy.BaseInstructions == nil {
		params["baseInstructions"] = baseline
	}
	result, err := x.callScopedThread(ctx, "thread/fork", params, profile)
	if err != nil {
		return err
	}
	if result.Thread.ID == "" {
		return errors.New("engine did not return the reconfigured conversation")
	}
	previous := s.EngineID
	s.EngineID = result.Thread.ID
	s.ExecutionScopeID = profile.ID()
	s.BaseVersion = policy.BaseVersion(s.Policy)
	s.ModelProvider = result.ModelProvider
	if result.Model != "" {
		s.Model = result.Model
	}
	if err = x.save(s); err != nil {
		return err
	}
	return x.emit(s.ID, "engine.reconfigured", map[string]string{"previousEngineId": previous, "engineId": s.EngineID, "reason": "base instructions changed; history preserved"})
}
