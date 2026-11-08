package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"mewdex/internal/attachments"
	"mewdex/internal/computeruse"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/lifecycle"
	"mewdex/internal/policy"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type Executor struct {
	computer           *computeruse.Manager
	terminal           *terminalState
	config             config.Config
	store              *store.Store
	engine             engine.Engine
	mu                 sync.Mutex
	projectMu          sync.RWMutex
	locks              map[string]*sync.Mutex
	loaded             map[string]bool
	workspaceOwners    map[string]string
	projectRoots       map[string]string
	backgroundWatches  map[string]bool
	operations         map[string]context.CancelFunc
	stopRequests       map[string]int
	requests           map[string]pendingRequest
	filePreviews       map[string]filePreviewEntry
	notify             func(protocol.Event)
	attachments        *attachments.Store
	fetchAttachments   func(context.Context, []protocol.Input) error
	filePublisher      FilePublisher
	active             sync.WaitGroup
	closing            bool
	bootSession        string
	scopeMu            sync.Mutex
	views              map[string]*attachments.View
	inputParents       map[string]*attachments.View
	attachmentContexts map[string]attachmentContext
	done               chan struct{}
}

func New(c config.Config, s *store.Store, e engine.Engine, notify func(protocol.Event)) (*Executor, error) {
	x := &Executor{config: c, store: s, engine: e, locks: map[string]*sync.Mutex{}, loaded: map[string]bool{}, workspaceOwners: map[string]string{}, projectRoots: map[string]string{}, backgroundWatches: map[string]bool{}, operations: map[string]context.CancelFunc{}, stopRequests: map[string]int{}, requests: map[string]pendingRequest{}, filePreviews: map[string]filePreviewEntry{}, notify: notify, done: make(chan struct{}), views: map[string]*attachments.View{}, inputParents: map[string]*attachments.View{}}
	x.bootSession, _ = lifecycle.BootSession()
	sessions, err := store.List[protocol.Session](s, "sessions")
	if err != nil {
		return nil, err
	}
	for _, ss := range sessions {
		if ss.DeviceID != c.DeviceID {
			return nil, errors.New("executor data belongs to a different device")
		}
	}
	if err := x.bindWorkspaces(); err != nil {
		return nil, err
	}
	assets, err := attachments.New(filepath.Join(c.DataDir, "attachments"))
	if err != nil {
		return nil, err
	}
	x.attachments = assets
	x.attachmentContexts = map[string]attachmentContext{}
	for _, ss := range sessions {
		recovery, err := s.ExecutionRecovery(ss)
		if err != nil {
			return nil, err
		}
		ss.ExecutionRecovery = recovery
		retired := recovery != nil && recovery.State == "retired"
		if ss.BaseVersion == "" {
			ss.BaseVersion = policy.BaseVersion(ss.Policy)
		}
		if ss.PolicyInstructions == "" {
			setPolicy(&ss, ss.Policy)
		}
		if ss.DefaultModel == "" {
			ss.DefaultModel = ss.Model
		}
		if !ss.EngineStarted {
			started, err := s.HasStarted(ss.ID)
			if err != nil {
				return nil, err
			}
			if started {
				ss.EngineStarted = true
				if err = s.Put("sessions", ss.ID, ss); err != nil {
					return nil, err
				}
			}
		}
		if !retired && ss.ExecutionUnconfirmed() {
			ss.State = "unknown"
			ss.Frozen = true
			if ss.EngineStarted && !retired {
				x.restoreProjectClaim(ss)
			}
			ss.UpdatedAt = protocol.Now()
			if err = s.Put("sessions", ss.ID, ss); err != nil {
				return nil, err
			}
		}
		if err = x.decorateRecovery(&ss); err != nil {
			return nil, err
		}
		if err = s.Put("sessions", ss.ID, ss); err != nil {
			return nil, err
		}
		if err = x.emit(ss.ID, "session.updated", ss); err != nil {
			return nil, err
		}
		if err = x.snapshot(ss); err != nil {
			return nil, err
		}
	}
	if err = x.recoverQueue(sessions); err != nil {
		return nil, err
	}
	recovered, err := s.RecoverCommands(c.DeviceID)
	if err != nil {
		return nil, err
	}
	for _, result := range recovered {
		command, err := s.Command(result.CommandID)
		if err != nil {
			return nil, err
		}
		if err = x.emit(command.SessionID, "command.result", result); err != nil {
			return nil, err
		}
	}
	// RPC callbacks belong to the engine process, so persisted pending requests
	// cannot be answered after an executor restart.
	interactions, err := store.List[protocol.Interaction](s, "interactions")
	if err != nil {
		return nil, err
	}
	for _, request := range interactions {
		if request.State == "pending" || request.State == "responding" {
			request.State = "expired"
			if err = x.recordInteraction(request); err != nil {
				return nil, err
			}
		}
	}
	var restored map[string]bool
	if err = s.Get("maintenance", "restore_pending", &restored); err == nil && restored["pending"] {
		// The previous process's state notifications were retired during restore.
		// Re-publish terminal callback state before reconnecting to the Hub.
		for _, request := range interactions {
			if err = s.Get("interactions", request.ID, &request); err != nil {
				return nil, err
			}
			if err = x.recordInteraction(request); err != nil {
				return nil, err
			}
		}
		if err = s.Delete("maintenance", "restore_pending"); err != nil {
			return nil, err
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	go x.consume()
	return x, nil
}

func (x *Executor) SetAttachmentFetcher(fetch func(context.Context, []protocol.Input) error) {
	x.fetchAttachments = fetch
}
func (x *Executor) SetNotifier(notify func(protocol.Event)) {
	x.mu.Lock()
	x.notify = notify
	x.mu.Unlock()
}
func (x *Executor) lock(id string) *sync.Mutex {
	x.mu.Lock()
	defer x.mu.Unlock()
	l := x.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		x.locks[id] = l
	}
	return l
}
func (x *Executor) emit(session, kind string, value any) error {
	e := protocol.Event{ID: protocol.ID(), DeviceID: x.config.DeviceID, SessionID: session, Kind: kind, Payload: protocol.JSON(value), CreatedAt: protocol.Now()}
	stored, err := x.store.Append(e)
	if err != nil {
		return err
	}
	x.publish(stored)
	return nil
}
func (x *Executor) publish(stored protocol.Event) {
	x.mu.Lock()
	notify := x.notify
	x.mu.Unlock()
	if notify != nil {
		notify(stored)
	}
}
func (x *Executor) save(s *protocol.Session) error {
	if err := x.decorateRecovery(s); err != nil {
		return err
	}
	s.UpdatedAt = protocol.Now()
	if err := x.store.Put("sessions", s.ID, s); err != nil {
		return err
	}
	return x.emit(s.ID, "session.updated", s)
}
func (x *Executor) project(id string) (protocol.Project, error) {
	x.projectMu.RLock()
	defer x.projectMu.RUnlock()
	for _, p := range x.config.Projects {
		if p.ID == id {
			return p, nil
		}
	}
	return protocol.Project{}, errors.New("project is not configured on this executor")
}

func (x *Executor) Handle(ctx context.Context, c protocol.Command) protocol.CommandResult {
	x.mu.Lock()
	if x.closing {
		x.mu.Unlock()
		return protocol.CommandResult{CommandID: c.ID, State: "failed", Error: "executor is shutting down"}
	}
	x.active.Add(1)
	x.mu.Unlock()
	defer x.active.Done()
	if c.ID == "" || c.DeviceID != x.config.DeviceID {
		return protocol.CommandResult{CommandID: c.ID, State: "failed", Error: "invalid command ID or target device"}
	}
	prior, owner, err := x.store.Claim(c)
	if err != nil {
		return protocol.CommandResult{CommandID: c.ID, State: "failed", Error: err.Error()}
	}
	if !owner {
		return prior
	}
	if c.Kind == "stop" || c.Kind == "interrupt" {
		finish := x.prioritizeStop(c)
		defer finish()
	}
	r := x.execute(ctx, c)
	r.CommandID = c.ID
	if err = x.store.Finish(r); err != nil {
		r.State = "unknown"
		r.Error = "command ran but result persistence failed"
	}
	_ = x.emit(c.SessionID, "command.result", r)
	return r
}

func (x *Executor) execute(ctx context.Context, c protocol.Command) protocol.CommandResult {
	fail := commandFailure
	if strings.HasPrefix(c.Kind, "computer.") {
		return x.computerCommand(ctx, c)
	}
	if strings.HasPrefix(c.Kind, "terminal.") {
		return x.terminalCommand(ctx, c)
	}
	if strings.HasPrefix(c.Kind, "workspace.") {
		return x.workspaceCommand(c)
	}
	if x.terminal != nil {
		return fail(errors.New("对话和模型账号由 Hub 管理，请从 Hub 项目发起任务"))
	}
	if c.Kind == "model.list" {
		return x.modelList(ctx, c)
	}
	if strings.HasPrefix(c.Kind, "file.") {
		return x.fileCommand(ctx, c)
	}
	if strings.HasPrefix(c.Kind, "account.") {
		return x.accountCommand(ctx, c)
	}
	if c.Kind == "policy.inspect" && c.SessionID == "" {
		view, err := x.inspectPolicy(ctx, c.ProjectID, nil, c.Policy)
		if err != nil {
			return fail(err)
		}
		return protocol.CommandResult{State: "completed", Data: protocol.JSON(view)}
	}
	if c.Kind == "execution.retire" && (len(c.Input) > 0 || c.Policy != nil || len(c.Response) > 0) {
		return fail(errors.New("ending an old execution does not accept new input or policy changes"))
	}
	if x.fetchAttachments != nil {
		if err := x.fetchAttachments(ctx, c.Input); err != nil {
			return fail(err)
		}
	}
	if c.Kind == "create" {
		return x.create(ctx, c)
	}
	if c.SessionID == "" {
		return fail(errors.New("sessionId required"))
	}
	l := x.lock(c.SessionID)
	l.Lock()
	defer l.Unlock()
	var s protocol.Session
	if err := x.store.Get("sessions", c.SessionID, &s); err != nil {
		return fail(err)
	}
	if s.DeviceID != c.DeviceID {
		return fail(errors.New("session belongs to another device"))
	}
	if err := x.decorateRecovery(&s); err != nil {
		return fail(err)
	}
	if s.ExecutionRecovery != nil && s.ExecutionRecovery.State == "retired" && c.Kind != "execution.retire" && c.Kind != "requests" && c.Kind != "policy.inspect" {
		return fail(errors.New("this execution was ended with its original outcome preserved; open a new conversation to continue"))
	}
	if c.ExpectedTurnID != "" && c.ExpectedTurnID != s.TurnID {
		return fail(errors.New("active turn changed; refresh session before steering or stopping"))
	}
	var err error
	if c.Kind != "stop" && c.Kind != "interrupt" {
		var finish func()
		ctx, finish, err = x.operation(ctx, s.ID)
		if err != nil {
			return fail(err)
		}
		defer finish()
	}
	var data json.RawMessage
	switch c.Kind {
	case "execution.retire":
		if err = x.retireExecution(&s, c); err != nil {
			return fail(err)
		}
	case "start":
		if s.Frozen {
			return fail(errors.New("task queue is frozen; resume explicitly"))
		}
		if s.State != "idle" && s.State != "failed" {
			return fail(errors.New("session is not idle; choose steer or enqueue"))
		}
		if c.Policy != nil {
			if c.ExpectedPolicyVersion != "" && c.ExpectedPolicyVersion != s.PolicyVersion {
				return fail(errors.New("policy changed on another input; refresh before starting"))
			}
			var next protocol.Policy
			next, err = policy.Normalize(*c.Policy)
			if err != nil {
				return fail(err)
			}
			if string(protocol.JSON(next)) != string(protocol.JSON(s.Policy)) {
				setPolicy(&s, next)
			}
		}
		if err = x.start(ctx, &s, c); err != nil {
			return fail(err)
		}
	case "steer":
		if s.State != "running" || s.TurnID == "" {
			return fail(errors.New("no active turn; send a new start command"))
		}
		if _, err = x.executionScope(s); err != nil {
			return fail(err)
		}
		input, e := x.input(s, c.Input)
		if e != nil {
			return fail(e)
		}
		err = x.engine.Call(ctx, "turn/steer", map[string]any{"threadId": s.EngineID, "expectedTurnId": s.TurnID, "input": input}, &data)
		if err == nil {
			err = x.emit(s.ID, "message.user", c)
			if err != nil {
				err = &engine.UnknownOutcome{Cause: err}
			}
		}
	case "enqueue":
		var item protocol.QueueItem
		item, err = x.enqueue(s, c)
		data = protocol.JSON(item)
		if err == nil {
			if !s.Frozen && s.State == "idle" {
				go x.advance(s.ID)
			}
		}
	case "queue.cancel":
		var item protocol.QueueItem
		item, err = x.cancelQueued(s, c)
		data = protocol.JSON(item)
	case "interrupt", "stop":
		err = x.stop(ctx, &s, c.Kind)
	case "reconcile":
		var view executionView
		view, err = x.reconcile(ctx, &s)
		data = protocol.JSON(view)
	case "resume":
		if s.State == "unknown" || s.StopStatus == "unconfirmed" || s.StopStatus == "processes-unchecked" {
			return fail(errors.New("execution state is unconfirmed; retry stop before resuming the queue"))
		}
		queue, queueErr := x.store.QueueItems(s.ID)
		if queueErr != nil {
			return fail(queueErr)
		}
		for _, item := range queue {
			if item.State == "unknown" || item.State == "dispatching" || item.State == "running" {
				return fail(errors.New("a queued execution is still unconfirmed; reconcile its original turn before resuming"))
			}
		}
		if s.State == "running" || s.State == "stopping" {
			return fail(errors.New("cannot resume an active or stopping turn"))
		}
		err = x.load(ctx, &s)
		if err == nil {
			s.Frozen = false
			s.State = "idle"
			s.TurnID = ""
			err = x.save(&s)
			if err == nil {
				go x.advance(s.ID)
			}
		}
	case "history":
		err = x.load(ctx, &s)
		if err == nil {
			err = x.engine.Call(ctx, "thread/turns/list", map[string]any{"threadId": s.EngineID, "limit": 50}, &data)
		}
	case "respond":
		err = x.respond(ctx, s, c)
	case "requests":
		data = protocol.JSON(x.pendingInteractions(s.ID))
	case "policy.inspect":
		var inspection protocol.PromptInspection
		inspection, err = x.inspectPolicy(ctx, s.ProjectID, &s, c.Policy)
		data = protocol.JSON(inspection)
	case "policy.save", "policy.restore":
		err = x.editPolicy(&s, c)
	default:
		return fail(errors.New("unsupported command kind"))
	}
	if err != nil {
		return fail(err)
	}
	return protocol.CommandResult{State: "completed", Session: &s, Data: data}
}

func (x *Executor) create(ctx context.Context, c protocol.Command) protocol.CommandResult {
	fail := commandFailure
	_, err := x.project(c.ProjectID)
	if err != nil {
		return fail(err)
	}
	pol := protocol.Policy{}
	if c.Policy != nil {
		pol = *c.Policy
	}
	pol, err = policy.Normalize(pol)
	if err != nil {
		return fail(err)
	}
	s := protocol.Session{ID: "s_" + c.ID, DeviceID: c.DeviceID, ProjectID: c.ProjectID, Title: c.Title, State: "idle", Policy: pol, BaseVersion: policy.BaseVersion(pol), CreatedAt: protocol.Now()}
	profile, err := x.executionScope(s)
	if err != nil {
		return fail(err)
	}
	params := x.threadParams(pol, profile)
	reply, err := x.callScopedThread(ctx, "thread/start", params, profile)
	if err != nil {
		return fail(err)
	}
	if reply.Thread.ID == "" {
		return fail(&engine.UnknownOutcome{Cause: errors.New("engine created a conversation without its identifier")})
	}
	s.EngineID, s.ExecutionScopeID = reply.Thread.ID, profile.ID()
	s.ScopeAccessMode = pol.AccessMode
	s.Model, s.DefaultModel = reply.Model, reply.Model
	s.ModelProvider = reply.ModelProvider
	setPolicy(&s, pol)
	if s.Title == "" {
		s.Title = "新对话"
	}
	x.mu.Lock()
	x.loaded[s.ID] = true
	x.mu.Unlock()
	if err = x.save(&s); err != nil {
		return fail(err)
	}
	if err = x.snapshot(s); err != nil {
		return fail(err)
	}
	return protocol.CommandResult{State: "completed", Session: &s}
}

func (x *Executor) load(ctx context.Context, s *protocol.Session) error {
	x.mu.Lock()
	loaded := x.loaded[s.ID]
	x.mu.Unlock()
	if loaded {
		profile, err := x.executionScope(*s)
		if err != nil || profile.ID() == s.ExecutionScopeID {
			return err
		}
	}
	// Older development records predate engineStarted; infer only from persisted evidence.
	if !s.EngineStarted {
		started, err := x.store.HasStarted(s.ID)
		if err != nil {
			return err
		}
		s.EngineStarted = started
	}
	if s.EngineStarted {
		profile, err := x.executionScope(*s)
		if err != nil {
			return err
		}
		params := scopeParams(profile)
		params["threadId"], params["excludeTurns"] = s.EngineID, true
		data, err := x.callScopedThread(ctx, "thread/resume", params, profile)
		if err != nil {
			return err
		}
		if data.Thread.ID != s.EngineID {
			return &engine.UnknownOutcome{Cause: errors.New("engine resumed a different conversation")}
		}
		s.ExecutionScopeID = profile.ID()
		s.ScopeAccessMode = s.Policy.AccessMode
		s.ModelProvider = data.ModelProvider
		if data.Model != "" {
			s.Model = data.Model
			if s.DefaultModel == "" {
				s.DefaultModel = data.Model
			}
		}
	} else if err := x.replaceUnstarted(ctx, s); err != nil {
		return err
	}
	if err := x.save(s); err != nil {
		return err
	}
	x.mu.Lock()
	x.loaded[s.ID] = true
	x.mu.Unlock()
	return nil
}

func (x *Executor) replaceUnstarted(ctx context.Context, s *protocol.Session) error {
	profile, err := x.executionScope(*s)
	if err != nil {
		return err
	}
	params := x.threadParams(s.Policy, profile)
	result, err := x.callScopedThread(ctx, "thread/start", params, profile)
	if err != nil {
		return err
	}
	if result.Thread.ID == "" {
		return &engine.UnknownOutcome{Cause: errors.New("engine replaced a conversation without its identifier")}
	}
	s.EngineID = result.Thread.ID
	s.ExecutionScopeID = profile.ID()
	s.ScopeAccessMode = s.Policy.AccessMode
	s.ModelProvider = result.ModelProvider
	if result.Model != "" {
		s.Model = result.Model
		if s.DefaultModel == "" {
			s.DefaultModel = result.Model
		}
	}
	s.BaseVersion = policy.BaseVersion(s.Policy)
	return nil
}
func (x *Executor) input(s protocol.Session, in []protocol.Input) ([]map[string]any, error) {
	return x.prepareInput(s, in, true)
}

func (x *Executor) validateInput(projectID string, in []protocol.Input) error {
	_, err := x.prepareInput(protocol.Session{ProjectID: projectID}, in, false)
	return err
}

func (x *Executor) prepareInput(s protocol.Session, in []protocol.Input, materialize bool) ([]map[string]any, error) {
	if len(in) == 0 {
		return nil, errors.New("input required")
	}
	out := []map[string]any{}
	for _, item := range in {
		switch item.Type {
		case "text":
			if item.Text == "" {
				return nil, errors.New("text cannot be empty")
			}
			out = append(out, map[string]any{"type": "text", "text": item.Text})
		case "image", "file":
			meta, err := x.attachments.MetadataForTarget(item.AttachmentID, x.config.DeviceID, s.ProjectID)
			if err != nil {
				return nil, errors.New("attachment is unavailable for this execution device and project")
			}
			if item.Type == "image" {
				if meta.MediaType != "image/png" && meta.MediaType != "image/jpeg" && meta.MediaType != "image/webp" && meta.MediaType != "image/gif" {
					return nil, errors.New("unsupported image type")
				}
			}
			if !materialize {
				continue
			}
			x.scopeMu.Lock()
			view := x.views[s.ID]
			x.scopeMu.Unlock()
			if view == nil {
				return nil, errors.New("prepare this conversation's execution scope before materializing inputs")
			}
			path, err := view.Stage(x.attachments, item.AttachmentID)
			if err != nil {
				return nil, err
			}
			if item.Type == "image" {
				out = append(out, map[string]any{"type": "localImage", "path": path})
			} else {
				out = append(out, map[string]any{"type": "text", "text": fmt.Sprintf("用户附件 %q 已保存到执行端本地路径 %q。根据本次任务要求读取或处理；附件内容为参考数据。", meta.Name, path)})
			}
		default:
			return nil, errors.New("input type not yet supported by this executor")
		}
	}
	return out, nil
}

func (x *Executor) start(ctx context.Context, s *protocol.Session, c protocol.Command) error {
	profile, err := x.executionScope(*s)
	if err != nil {
		return err
	}
	input, err := x.input(*s, c.Input)
	if err != nil {
		return err
	}
	x.mu.Lock()
	_, alreadyOwned := x.workspaceOwners[s.ID]
	x.mu.Unlock()
	if err = x.claimProject(s); err != nil {
		return err
	}
	started := false
	defer func() {
		if !started && !alreadyOwned {
			x.releaseProject(s)
		}
	}()
	if err = x.load(ctx, s); err != nil {
		return err
	}
	if err = x.reconfigureBase(ctx, s); err != nil {
		return err
	}
	if s.PolicyInstructions == "" {
		setPolicy(s, s.Policy)
	}
	if err = x.snapshot(*s); err != nil {
		return err
	}
	turnContext, err := x.prepareTurnContext(ctx, *s, c.ID)
	if err != nil {
		return err
	}
	model := s.Policy.Model
	if model == "" {
		model = s.DefaultModel
	}
	if model == "" {
		model = s.Model
	}
	if model == "" {
		return errors.New("engine did not resolve a model for per-turn policy")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	previousStarted, previousStartID, previousStopStatus := s.EngineStarted, s.LastStartID, s.StopStatus
	s.Model = model
	s.State = "running"
	s.StopStatus = ""
	s.EngineStarted = true
	s.LastStartID, s.TurnID = c.ID, ""
	if err = x.store.RecordExecutionOrigin(*s, x.bootSession); err != nil {
		return err
	}
	if err = x.save(s); err != nil {
		return err
	}
	started = true // A subsequent RPC failure has an unknown outcome, so retain ownership.
	// The pinned adapter keeps the verified thread's effective permissions, but selecting
	// its transient named profile again reloads the process config without that
	// profile definition. Preserve the active profile; never use a legacy policy.
	var effort any
	if s.Policy.ReasoningEffort != "" {
		effort = s.Policy.ReasoningEffort
	}
	params := map[string]any{"threadId": s.EngineID, "input": input, "clientUserMessageId": c.ID, "cwd": profile.Workspace(), "approvalPolicy": "on-request", "approvalsReviewer": "user", "collaborationMode": map[string]any{"mode": policy.CollaborationMode(s.Policy), "settings": map[string]any{"model": model, "reasoning_effort": effort, "developer_instructions": turnContext.DeveloperInstructions}}}
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = x.engine.Call(ctx, "turn/start", params, &reply); err != nil {
		turnContext.State = "rejected"
		s.State = "failed"
		if engine.IsUnknownOutcome(err) {
			turnContext.State = "unknown"
			s.State = "unknown"
		} else {
			started = false
			s.EngineStarted, s.LastStartID = previousStarted, previousStartID
			s.StopStatus = previousStopStatus
		}
		s.Frozen = true
		_ = x.store.Put("turn_contexts", c.ID, turnContext)
		_ = x.save(s)
		return err
	}
	if reply.Turn.ID == "" {
		turnContext.State = "unknown"
		_ = x.store.Put("turn_contexts", c.ID, turnContext)
		s.State, s.Frozen = "unknown", true
		_ = x.save(s)
		return &engine.UnknownOutcome{Cause: errors.New("engine accepted a turn without its identifier")}
	}
	s.TurnID = reply.Turn.ID
	turnContext.State = "submitted"
	turnContext.TurnID = reply.Turn.ID
	if err = x.store.Put("turn_contexts", c.ID, turnContext); err != nil {
		return &engine.UnknownOutcome{Cause: err}
	}
	if err = x.save(s); err != nil {
		return &engine.UnknownOutcome{Cause: err}
	}
	if err = x.emit(s.ID, "message.user", c); err != nil {
		return &engine.UnknownOutcome{Cause: err}
	}
	return nil
}

func (x *Executor) consume() {
	defer close(x.done)
	for n := range x.engine.Notifications() {
		x.consumeOne(n)
	}
	_ = x.expireInteractions("", "", nil)
	sessions, _ := store.List[protocol.Session](x.store, "sessions")
	for _, s := range sessions {
		l := x.lock(s.ID)
		l.Lock()
		if s.State == "running" || s.State == "stopping" {
			_ = x.observeQueue(s.ID, s.TurnID, "unknown", "engine disconnected; no automatic replay")
			s.State = "unknown"
			s.Frozen = true
			_ = x.save(&s)
		}
		l.Unlock()
	}
}
func (x *Executor) Close() error {
	x.mu.Lock()
	x.closing = true
	cancels := make([]context.CancelFunc, 0, len(x.operations))
	for _, cancel := range x.operations {
		cancels = append(cancels, cancel)
	}
	x.mu.Unlock()
	if x.terminal != nil {
		x.terminal.cancel()
		if x.computer != nil {
			x.computer.Close()
		}
		x.active.Wait()
		x.terminal.mu.Lock()
		runs := make([]*terminalRun, 0, len(x.terminal.runs))
		for _, run := range x.terminal.runs {
			runs = append(runs, run)
		}
		x.terminal.mu.Unlock()
		for _, run := range runs {
			<-run.done
		}
		return nil
	}
	for _, cancel := range cancels {
		cancel()
	}
	err := x.engine.Close()
	x.active.Wait()
	<-x.done
	return err
}

func (x *Executor) releaseProject(s *protocol.Session) {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.workspaceOwners, s.ID)
}

func (x *Executor) consumeOne(n engine.Notification) {
	if len(n.RequestID) > 0 {
		x.receiveRequest(n)
		return
	}
	if strings.HasPrefix(n.Method, "account/") {
		x.accountNotification(n)
		return
	}
	var params struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(n.Params, &params)
	sessions, err := store.List[protocol.Session](x.store, "sessions")
	if err != nil {
		return
	}
	for _, old := range sessions {
		if old.EngineID != params.ThreadID {
			continue
		}
		l := x.lock(old.ID)
		l.Lock()
		var s protocol.Session
		if err = x.store.Get("sessions", old.ID, &s); err != nil {
			l.Unlock()
			return
		}
		if s.ExecutionRecovery != nil && s.ExecutionRecovery.State == "retired" {
			l.Unlock()
			continue
		}
		kind := "engine." + n.Method
		value := any(n.Params)
		if err := x.emit(s.ID, kind, value); err == nil {
			x.captureFilePreview(s, n)
		}
		if n.Method == "thread/tokenUsage/updated" {
			var usage struct {
				TokenUsage struct {
					Last               protocol.ContextUsage `json:"last"`
					ModelContextWindow *int64                `json:"modelContextWindow"`
				} `json:"tokenUsage"`
			}
			if json.Unmarshal(n.Params, &usage) == nil {
				s.ContextUsage = &usage.TokenUsage.Last
				s.ContextUsage.ModelContextWindow = usage.TokenUsage.ModelContextWindow
				_ = x.save(&s)
			}
		}
		if n.Method == "serverRequest/resolved" {
			var resolved struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(n.Params, &resolved) == nil {
				_ = x.expireInteractions(s.ID, "", resolved.RequestID)
			}
		}
		if n.Method == "turn/started" {
			if s.TurnID == "" || params.Turn.ID != s.TurnID {
				l.Unlock()
				return
			}
			_ = x.observeQueue(s.ID, params.Turn.ID, "running", "")
			s.TurnID = params.Turn.ID
			if s.State != "stopping" {
				s.State = "running"
			}
			_ = x.save(&s)
		}
		if n.Method == "turn/completed" {
			if params.Turn.ID == "" || s.TurnID == "" || params.Turn.ID != s.TurnID {
				l.Unlock()
				return
			}
			queueState := params.Turn.Status
			if queueState != "failed" && queueState != "interrupted" {
				queueState = "completed"
			}
			_ = x.observeQueue(s.ID, params.Turn.ID, queueState, "")
			_ = x.expireInteractions(s.ID, params.Turn.ID, nil)
			s.TurnID = ""
			if s.StopStatus == "unconfirmed" {
				s.State = "unknown"
			} else if s.Frozen {
				s.State = "paused"
			} else if params.Turn.Status == "failed" {
				s.State = "failed"
			} else {
				s.State = "idle"
			}
			if s.StopStatus != "unconfirmed" && s.StopStatus != "turn-only" {
				x.completedWorkspace(&s)
			}
			_ = x.save(&s)
			if !s.Frozen && s.StopStatus != "background-running" {
				go x.advance(s.ID)
			}
		}
		l.Unlock()
		return
	}
}
