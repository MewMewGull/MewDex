package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mewdex/internal/attachments"
	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executionscope"
	"mewdex/internal/projectfiles"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/workspace"
)

type terminalRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type terminalState struct {
	ctx     context.Context
	cancel  context.CancelFunc
	sandbox *engine.Sandbox
	mu      sync.Mutex
	runs    map[string]*terminalRun
}

// NewTerminal is the model-free execution-node entry point. Workspace bindings,
// command receipts and process results remain durable across reconnections.
func NewTerminal(ctx context.Context, c config.Config, s *store.Store) (*Executor, error) {
	if c.Engine.Home != "" {
		if _, err := os.Stat(c.Engine.Home); err == nil {
			c.PrivatePaths = append(c.PrivatePaths, c.Engine.Home)
		}
	}
	c.Engine.Home = filepath.Join(c.DataDir, "terminal-runtime")
	sandbox, err := engine.NewSandbox(ctx, c.Engine)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	x := &Executor{
		config:       c,
		store:        s,
		projectRoots: map[string]string{},
		locks:        map[string]*sync.Mutex{},
		operations:   map[string]context.CancelFunc{},
		stopRequests: map[string]int{},
		terminal: &terminalState{
			ctx:     ctx,
			cancel:  cancel,
			sandbox: sandbox,
			runs:    map[string]*terminalRun{},
		},
	}
	x.attachments, err = attachments.New(filepath.Join(c.DataDir, "terminal-attachments"))
	if err != nil {
		cancel()
		return nil, err
	}
	if err = x.bindWorkspaces(); err != nil {
		cancel()
		return nil, err
	}
	jobs, err := store.List[protocol.TerminalJob](s, "terminal_jobs")
	if err != nil {
		cancel()
		return nil, err
	}
	for _, job := range jobs {
		if job.State == "running" {
			job.State, job.Error, job.UpdatedAt = "unknown", "执行端已重启，原指令的结果尚未确认", protocol.Now()
			if err = s.Put("terminal_jobs", job.ID, job); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	_, err = s.RecoverCommands(c.DeviceID)
	if err != nil {
		cancel()
		return nil, err
	}
	return x, nil
}

func (x *Executor) terminalCommand(ctx context.Context, c protocol.Command) protocol.CommandResult {
	r := c.Terminal
	if x.terminal == nil || r == nil || r.OwnerID == "" || c.SessionID != "" {
		return commandFailure(errors.New("Hub execution request required"))
	}
	switch c.Kind {
	case "terminal.file.list", "terminal.file.export":
		if c.File == nil {
			return commandFailure(errors.New("file request required"))
		}
		project, err := x.project(c.ProjectID)
		if err != nil {
			return commandFailure(err)
		}
		scope, err := x.terminalProfile(project, []string{project.ID}, "read-only")
		if err != nil {
			return commandFailure(err)
		}
		root := projectfiles.New(scope.Workspace(), scope.PrivatePaths())
		if c.Kind == "terminal.file.list" {
			page, err := root.List(ctx, c.File.Path, c.File.After)
			if err != nil {
				return commandFailure(err)
			}
			return protocol.CommandResult{State: "completed", Data: protocol.JSON(page)}
		}
		if c.RequestID == "" {
			return commandFailure(errors.New("original export request required"))
		}
		c.ID, c.Kind = c.RequestID, "file.export"
		return x.exportProjectSnapshot(ctx, c, protocol.Session{ID: r.OwnerID, DeviceID: c.DeviceID, ProjectID: c.ProjectID}, root)
	case "terminal.exec":
		if r.YieldMS < 0 || r.YieldMS > 20000 {
			return commandFailure(errors.New("yieldMs must be between 0 and 20000"))
		}
		job, err := x.startTerminal(c)
		if err != nil {
			return commandFailure(err)
		}
		return x.waitTerminal(ctx, job, r.YieldMS)
	case "terminal.wait", "terminal.stop":
		var job protocol.TerminalJob
		if err := x.store.Get("terminal_jobs", r.JobID, &job); err != nil || job.OwnerID != r.OwnerID || job.ProjectID != c.ProjectID {
			return commandFailure(errors.New("此指令不属于当前项目对话"))
		}
		if c.Kind == "terminal.stop" {
			x.terminal.mu.Lock()
			if run := x.terminal.runs[job.ID]; run != nil {
				run.cancel()
			}
			x.terminal.mu.Unlock()
		}
		return x.waitTerminal(ctx, job, r.YieldMS)
	case "terminal.stopOwner":
		x.terminal.mu.Lock()
		if err := x.store.Put("terminal_stopped_owners", r.OwnerID, true); err != nil {
			x.terminal.mu.Unlock()
			return commandFailure(err)
		}
		jobs, err := store.List[protocol.TerminalJob](x.store, "terminal_jobs")
		if err != nil {
			x.terminal.mu.Unlock()
			return commandFailure(err)
		}
		runs := []*terminalRun{}
		for _, job := range jobs {
			if job.OwnerID != r.OwnerID {
				continue
			}
			if run := x.terminal.runs[job.ID]; run != nil {
				run.cancel()
				runs = append(runs, run)
			}
		}
		x.terminal.mu.Unlock()
		for _, run := range runs {
			select {
			case <-run.done:
			case <-ctx.Done():
				return commandFailure(errors.New("执行进程的停止结果尚未确认"))
			}
		}
		jobs, err = store.List[protocol.TerminalJob](x.store, "terminal_jobs")
		if err != nil {
			return commandFailure(err)
		}
		for _, job := range jobs {
			if job.OwnerID == r.OwnerID && (job.State == "running" || job.State == "unknown") {
				return commandFailure(errors.New("原执行结果尚未确认"))
			}
		}
		return protocol.CommandResult{State: "completed", Data: protocol.JSON(map[string]bool{"stopped": true})}
	default:
		return commandFailure(errors.New("unsupported execution-node operation"))
	}
}

func (x *Executor) terminalProfile(project protocol.Project, ids []string, accessMode string) (executionscope.Profile, error) {
	environment, err := x.terminal.sandbox.Environment()
	if err != nil {
		return executionscope.Profile{}, err
	}
	private := append([]string{x.config.DataDir}, environment.Private...)
	private = append(private, x.config.PrivatePaths...)
	for _, path := range []string{x.config.NodeTokenFile} {
		if path != "" {
			private = append(private, path)
		}
	}
	roots, err := x.terminalRoots(ids)
	if err != nil {
		return executionscope.Profile{}, err
	}
	if x.config.ProfilePath != "" {
		for _, root := range roots {
			path, err := executionscope.PrivateProfilePath(root, x.config.ProfilePath)
			if err != nil {
				return executionscope.Profile{}, err
			}
			private = append(private, path)
		}
	}
	return executionscope.Build(executionscope.Spec{
		DeviceID:      x.config.DeviceID,
		ProjectID:     project.ID,
		EngineVersion: engine.CompatibleVersion,
		Workspace:     project.Path,
		Workspaces:    roots,
		ReadOnly:      environment.RuntimeReadOnly,
		Private:       private,
		AccessMode:    accessMode,
	})
}

func (x *Executor) terminalRoots(ids []string) ([]string, error) {
	roots := []string{}
	for _, id := range ids {
		project, err := x.project(id)
		if err != nil {
			return nil, err
		}
		root, err := workspace.CanonicalDir(project.Path)
		if err != nil || root != x.projectRoot(id) {
			return nil, errors.New("项目目录绑定已改变")
		}
		roots = append(roots, root)
	}
	return roots, nil
}

func (x *Executor) startTerminal(c protocol.Command) (protocol.TerminalJob, error) {
	r := c.Terminal
	job := protocol.TerminalJob{ID: c.ID, OwnerID: r.OwnerID, ProjectID: c.ProjectID, State: "running", CreatedAt: protocol.Now(), UpdatedAt: protocol.Now()}
	job.WorkspaceIDs = append([]string{c.ProjectID}, r.WorkspaceIDs...)
	if len(job.WorkspaceIDs) > 101 {
		return job, errors.New("项目目录过多")
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > int64((12*time.Hour)/time.Millisecond) || len(r.Command) == 0 || len(r.Command) > 256 {
		return job, errors.New("invalid execution timeout or command argv")
	}
	project, err := x.project(c.ProjectID)
	if err != nil {
		return job, err
	}
	root, err := workspace.CanonicalDir(project.Path)
	if err != nil || root != x.projectRoot(project.ID) {
		return job, errors.New("执行目录绑定已改变")
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd = root
	} else if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(root, cwd)
	}
	cwd, err = workspace.CanonicalDir(cwd)
	relative, relativeErr := filepath.Rel(root, cwd)
	if err != nil || relativeErr != nil || !filepath.IsLocal(relative) {
		return job, errors.New("工作目录必须位于所选项目目录内")
	}
	job.Cwd = cwd
	profile, err := x.terminalProfile(project, job.WorkspaceIDs, r.AccessMode)
	if err != nil {
		return job, err
	}
	timeout := time.Duration(r.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = time.Hour
	}
	taskCtx, cancel := context.WithTimeout(x.terminal.ctx, timeout)
	command, err := x.terminal.sandbox.Command(taskCtx, profile, cwd, r.Command)
	if err != nil {
		cancel()
		return job, err
	}
	x.terminal.mu.Lock()
	defer x.terminal.mu.Unlock()
	var stopped bool
	if err := x.store.Get("terminal_stopped_owners", r.OwnerID, &stopped); err == nil && stopped {
		cancel()
		return job, errors.New("此回合已停止")
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		cancel()
		return job, err
	}
	jobs, err := store.List[protocol.TerminalJob](x.store, "terminal_jobs")
	if err != nil {
		cancel()
		return job, err
	}
	for _, running := range jobs {
		if running.OwnerID != r.OwnerID && (running.State == "running" || running.State == "unknown") {
			currentRoots, e := x.terminalRoots(job.WorkspaceIDs)
			otherRoots, otherErr := x.terminalRoots(append([]string{running.ProjectID}, running.WorkspaceIDs...))
			if e != nil || otherErr != nil {
				cancel()
				return job, errors.New("原任务的目录占用范围尚未确认")
			}
			for _, current := range currentRoots {
				for _, other := range otherRoots {
					if workspace.Overlap(current, other) {
						cancel()
						return job, errors.New("目录正被另一个运行中或结果未确认的任务占用")
					}
				}
			}
		}
	}
	if err = x.store.Put("terminal_jobs", job.ID, job); err != nil {
		cancel()
		return job, err
	}
	run := &terminalRun{cancel: cancel, done: make(chan struct{})}
	x.terminal.runs[job.ID] = run
	go x.runTerminal(taskCtx, cancel, command, job, run)
	return job, nil
}

type terminalOutput struct {
	mu        sync.Mutex
	text      []byte
	truncated bool
}

func (output *terminalOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	remaining := (256 << 10) - len(output.text)
	output.text = append(output.text, data[:min(len(data), remaining)]...)
	output.truncated = output.truncated || len(data) > remaining
	return len(data), nil
}
func (output *terminalOutput) read() (string, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return strings.ToValidUTF8(string(output.text), "�"), output.truncated
}

func (x *Executor) runTerminal(ctx context.Context, cancel context.CancelFunc, command *exec.Cmd, job protocol.TerminalJob, run *terminalRun) {
	defer cancel()
	defer close(run.done)
	stdout, stderr := &terminalOutput{}, &terminalOutput{}
	command.Stdout, command.Stderr = stdout, stderr
	process, err := childprocess.StartContext(ctx, command)
	if err == nil {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
	waiting:
		for {
			select {
			case <-process.Done():
				err = process.Wait()
				break waiting
			case <-ticker.C:
				previousOut, previousErr := job.Stdout, job.Stderr
				job.Stdout, job.Truncated = stdout.read()
				var truncated bool
				job.Stderr, truncated = stderr.read()
				job.Truncated = job.Truncated || truncated
				job.UpdatedAt = protocol.Now()
				if (previousOut != job.Stdout || previousErr != job.Stderr) && x.store.Put("terminal_jobs", job.ID, job) == nil {
					_ = x.emit("", "terminal.job.updated", job)
				}
			}
		}
	}
	job.Stdout, job.Truncated = stdout.read()
	var truncated bool
	job.Stderr, truncated = stderr.read()
	job.Truncated = job.Truncated || truncated
	job.State = "completed"
	if err != nil {
		job.State, job.Error = "failed", err.Error()
		if ctx.Err() != nil {
			job.State, job.Error = "stopped", ctx.Err().Error()
		}
	}
	if process != nil {
		if cleanupErr := process.Kill(); cleanupErr != nil {
			job.State, job.Error = "unknown", cleanupErr.Error()
		}
	}
	if command.ProcessState != nil {
		code := command.ProcessState.ExitCode()
		job.ExitCode = &code
	}
	job.UpdatedAt = protocol.Now()
	if err = x.store.Put("terminal_jobs", job.ID, job); err != nil {
		fmt.Fprintln(os.Stderr, "execution result persistence:", err)
	} else {
		_ = x.emit("", "terminal.job.updated", job)
	}
	x.terminal.mu.Lock()
	delete(x.terminal.runs, job.ID)
	x.terminal.mu.Unlock()
}

func (x *Executor) waitTerminal(ctx context.Context, job protocol.TerminalJob, yieldMS int64) protocol.CommandResult {
	if yieldMS < 0 || yieldMS > 20000 {
		return commandFailure(errors.New("yieldMs must be between 0 and 20000"))
	}
	x.terminal.mu.Lock()
	run := x.terminal.runs[job.ID]
	x.terminal.mu.Unlock()
	if run != nil && yieldMS > 0 {
		timer := time.NewTimer(time.Duration(yieldMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-run.done:
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	if err := x.store.Get("terminal_jobs", job.ID, &job); err != nil {
		return protocol.CommandResult{State: "unknown", Error: err.Error()}
	}
	if !utf8.ValidString(job.Stdout) {
		job.Stdout = strings.ToValidUTF8(job.Stdout, "�")
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(job)}
}
