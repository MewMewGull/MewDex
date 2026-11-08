package executor

import (
	"context"
	"errors"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (x *Executor) ComputerDisconnected() {
	if x.computer != nil {
		x.computer.Interrupt("Hub 连接已断开，窗口授权已结束")
	}
}

func (x *Executor) computerCommand(ctx context.Context, command protocol.Command) protocol.CommandResult {
	request := command.Computer
	if x.computer == nil || request == nil || request.OwnerID == "" || command.SourceID != "hub" || command.SessionID != "" {
		return commandFailure(errors.New("Computer Use requires the paired Hub execution path"))
	}
	if _, err := x.project(command.ProjectID); err != nil {
		return commandFailure(err)
	}
	var result protocol.ComputerResult
	var err error
	switch command.Kind {
	case "computer.request":
		x.mu.Lock()
		err = x.computerTurnAllowed(request)
		if err == nil {
			result.Status, err = x.computer.Request(request.OwnerID, command.ProjectID, command.ID, request.Reason)
		}
		x.mu.Unlock()
	case "computer.status":
		result.Status, err = x.computer.Status()
		if result.Status.Session != nil && (result.Status.Session.OwnerID != request.OwnerID || result.Status.Session.ProjectID != command.ProjectID) {
			result.Status.Session = nil
			result.Status.Reason = "桌面正由另一会话占用"
		}
	case "computer.stop":
		x.mu.Lock()
		if request.TurnID != "" {
			err = x.store.Put("computer_stopped_turns", request.OwnerID+":"+request.TurnID, true)
		}
		x.mu.Unlock()
		if err == nil {
			result.Status, err = x.computer.Stop(ctx, request.SessionID, request.OwnerID)
		}
	case "computer.observe", "computer.act":
		if err = x.computerTurnAllowed(request); err != nil {
			return commandFailure(err)
		}
		if command.Kind == "computer.observe" && request.Action != nil || command.Kind == "computer.act" && request.Action == nil {
			return commandFailure(errors.New("computer operation and action do not match"))
		}
		result, err = x.computer.Operate(ctx, *request, command.ID)
	default:
		err = errors.New("unsupported computer operation")
	}
	if err != nil {
		if command.Kind == "computer.act" || command.Kind == "computer.stop" && ctx.Err() != nil {
			status, statusErr := x.computer.Status()
			if statusErr == nil && status.Session != nil && (status.Session.LastActionID == command.ID || command.Kind == "computer.stop") {
				return protocol.CommandResult{State: "unknown", Error: err.Error(), Data: protocol.JSON(protocol.ComputerResult{Status: status})}
			}
		}
		return commandFailure(err)
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(result)}
}

func (x *Executor) computerTurnAllowed(request *protocol.ComputerRequest) error {
	if request.TurnID == "" {
		return errors.New("active Hub turn required")
	}
	var stopped bool
	err := x.store.Get("computer_stopped_turns", request.OwnerID+":"+request.TurnID, &stopped)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if stopped {
		return errors.New("此回合的窗口控制已停止")
	}
	return nil
}
