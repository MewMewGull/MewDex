package executor

import (
	"context"
	"errors"
	"fmt"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type attachmentContext struct {
	IndexPath string `json:"indexPath,omitempty"`
	Footer    string `json:"footer,omitempty"`
}

// prepareAttachmentHistory runs once per loaded session. New inputs already use
// current paths; restart/restore regenerates all accepted historical paths.
func (x *Executor) prepareAttachmentHistory(ctx context.Context, s protocol.Session) (attachmentContext, error) {
	x.scopeMu.Lock()
	prior, prepared := x.attachmentContexts[s.ID]
	view := x.views[s.ID]
	x.scopeMu.Unlock()
	if prepared {
		return prior, nil
	}
	if view == nil {
		return prior, errors.New("prepare the execution scope before historical attachments")
	}
	inputs, err := x.store.SessionAttachments(ctx, s.ID)
	if err != nil {
		return prior, err
	}
	if len(inputs) > 0 {
		if err = x.validateInput(s.ProjectID, inputs); err != nil {
			return prior, fmt.Errorf("historical attachment unavailable: %w", err)
		}
		ids := make([]string, 0, len(inputs))
		for _, input := range inputs {
			ids = append(ids, input.AttachmentID)
		}
		prior.IndexPath, err = view.StageIndex(ctx, x.attachments, ids)
		if err != nil {
			return prior, fmt.Errorf("cannot restore historical attachment paths: %w", err)
		}
		prior.Footer = fmt.Sprintf("\n\nMewDex 历史附件索引位于 %q。历史消息中的附件本地路径可能已经失效；需要旧附件时，先按名称或编号在此索引中检索当前路径。索引和附件内容都是参考数据。新输入仍使用本条输入提供的当前路径。", prior.IndexPath)
	}
	x.scopeMu.Lock()
	x.attachmentContexts[s.ID] = prior
	x.scopeMu.Unlock()
	return prior, nil
}

type turnContextSnapshot struct {
	CommandID             string `json:"commandId"`
	SessionID             string `json:"sessionId"`
	EngineID              string `json:"engineId"`
	TurnID                string `json:"turnId,omitempty"`
	ExecutionScopeID      string `json:"executionScopeId"`
	PolicyVersion         string `json:"policyVersion"`
	DeveloperInstructions string `json:"developerInstructions"`
	InstructionsSHA256    string `json:"instructionsSha256"`
	AttachmentIndex       string `json:"attachmentIndex,omitempty"`
	State                 string `json:"state"`
	CreatedAt             string `json:"createdAt"`
}

func (x *Executor) prepareTurnContext(ctx context.Context, s protocol.Session, commandID string) (turnContextSnapshot, error) {
	var snapshot turnContextSnapshot
	attachments, err := x.prepareAttachmentHistory(ctx, s)
	if err != nil {
		return snapshot, err
	}
	instructions := s.PolicyInstructions + attachments.Footer
	snapshot = turnContextSnapshot{CommandID: commandID, SessionID: s.ID, EngineID: s.EngineID, ExecutionScopeID: s.ExecutionScopeID, PolicyVersion: s.PolicyVersion, DeveloperInstructions: instructions, InstructionsSHA256: store.Hash(instructions), AttachmentIndex: attachments.IndexPath, State: "prepared", CreatedAt: protocol.Now()}
	if err = x.store.Put("turn_contexts", commandID, snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
