package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type Attempt struct {
	Command protocol.Command        `json:"command"`
	Result  *protocol.CommandResult `json:"result,omitempty"`
}

type CaseRun struct {
	ID        string `json:"id"`
	Model     Model  `json:"model"`
	CaseID    string `json:"caseId"`
	SessionID string `json:"sessionId,omitempty"`
	State     string `json:"state"`
	Trace     string `json:"trace,omitempty"`
	Grade     *Grade `json:"grade,omitempty"`
	Archived  bool   `json:"archived"`
}

type runState struct {
	ID          string                       `json:"id"`
	CreatedAt   string                       `json:"createdAt"`
	SuiteHash   string                       `json:"suiteHash"`
	HubDeviceID string                       `json:"hubDeviceId"`
	IdentityID  string                       `json:"identityId"`
	Coordinator string                       `json:"coordinator"`
	Project     protocol.Project             `json:"project"`
	Harness     protocol.ProjectHarnessState `json:"harness"`
	Runtime     *protocol.ModelRuntime       `json:"runtime,omitempty"`
	Cases       []CaseRun                    `json:"cases"`
	Commands    map[string]Attempt           `json:"commands"`
}

type Report struct {
	Format      int                          `json:"format"`
	RunID       string                       `json:"runId"`
	CreatedAt   string                       `json:"createdAt"`
	UpdatedAt   string                       `json:"updatedAt"`
	Suite       Suite                        `json:"suite"`
	SuiteHash   string                       `json:"suiteHash"`
	HubDeviceID string                       `json:"hubDeviceId"`
	Project     protocol.Project             `json:"project"`
	Harness     protocol.ProjectHarnessState `json:"harness"`
	Runtime     *protocol.ModelRuntime       `json:"runtime,omitempty"`
	Cases       []CaseRun                    `json:"cases"`
	Complete    bool                         `json:"complete"`
	Passed      bool                         `json:"passed"`
	Error       string                       `json:"error,omitempty"`
}

type runner struct {
	client   *hubClient
	database *store.Store
	state    runState
	suite    Suite
	output   string
	timeout  time.Duration
	interval time.Duration
}

var errRunMismatch = errors.New("evaluation directory belongs to another suite, Hub or input identity")

// Run resumes only commands already recorded in this output directory. A lost
// submission is queried by its original ID. It is never automatically resent.
func Run(ctx context.Context, connection Connection, suite Suite, output string, timeout time.Duration) (Report, error) {
	if err := suite.Validate(); err != nil {
		return Report{}, err
	}
	if timeout < time.Second || timeout > 30*time.Minute {
		return Report{}, errors.New("evaluation case timeout must be between one second and thirty minutes")
	}
	client, err := newClient(connection)
	if err != nil {
		return Report{}, err
	}
	lease, err := lifecycle.Acquire(output)
	if err != nil {
		return Report{}, err
	}
	defer lease.Close()
	database, err := store.Open(filepath.Join(output, "run.db"))
	if err != nil {
		return Report{}, err
	}
	defer database.Close()
	r := &runner{client: client, database: database, suite: suite, output: output, timeout: timeout, interval: time.Second}
	err = r.execute(ctx)
	report := r.report(err)
	if r.state.ID == "" || errors.Is(err, errRunMismatch) {
		return report, err
	}
	if writeErr := writeJSON(filepath.Join(output, "report.json"), report); writeErr != nil {
		return report, errors.Join(err, writeErr)
	}
	if writeErr := r.writeReview(report); writeErr != nil {
		return report, errors.Join(err, writeErr)
	}
	return report, err
}

func (r *runner) save() error { return r.database.Put("evaluation", "run", r.state) }

func (r *runner) execute(ctx context.Context) error {
	var server protocol.ServerInfo
	if err := r.client.request(ctx, "GET", "/health", nil, &server); err != nil {
		return err
	}
	if server.Name != "MewDex" || server.ProtocolVersion != protocol.Version || server.HubDeviceID == "" || r.client.connection.HubDeviceID != "" && r.client.connection.HubDeviceID != server.HubDeviceID {
		return errors.New("evaluation Hub identity or protocol changed")
	}
	var identity protocol.Identity
	if err := r.client.request(ctx, "GET", "/v1/me", nil, &identity); err != nil {
		return err
	}
	if identity.Role != "admin" && identity.Role != "client" || identity.ID == "" || r.client.connection.Credentials.Identity.ID != "" && r.client.connection.Credentials.Identity.ID != identity.ID {
		return errors.New("evaluation input identity changed or is not an input client")
	}
	hash := store.Hash(string(protocol.JSON(r.suite)))
	err := r.database.Get("evaluation", "run", &r.state)
	if err == nil {
		if r.state.SuiteHash != hash || r.state.HubDeviceID != server.HubDeviceID || r.state.IdentityID != identity.ID {
			return errRunMismatch
		}
	} else if errors.Is(err, store.ErrNotFound) {
		r.state = runState{ID: "eval_" + protocol.ID(), CreatedAt: protocol.Now(), SuiteHash: hash, HubDeviceID: server.HubDeviceID, IdentityID: identity.ID, Commands: map[string]Attempt{}, Cases: []CaseRun{}}
		if err = r.save(); err != nil {
			return err
		}
	} else {
		return err
	}
	if r.state.Coordinator == "" {
		var devices []protocol.Device
		if err = r.client.request(ctx, "GET", "/v1/devices", nil, &devices); err != nil {
			return err
		}
		for _, device := range devices {
			if !slices.Contains(device.Roles, "coordinator") {
				continue
			}
			for _, project := range device.Projects {
				if project.ID == r.suite.ProjectID {
					r.state.Project, r.state.Coordinator = project, device.ID
				}
			}
		}
		if r.state.Coordinator == "" {
			return errors.New("evaluation project is not visible on the Hub coordinator")
		}
		if err = r.client.request(ctx, "GET", "/v1/projects/"+url.PathEscape(r.suite.ProjectID)+"/harness", nil, &r.state.Harness); err != nil {
			return err
		}
		if err = r.save(); err != nil {
			return err
		}
	}
	if r.state.Runtime == nil {
		result, err := r.command(ctx, protocol.Command{ID: r.state.ID + "_catalog", DeviceID: r.state.Coordinator, ProjectID: r.suite.ProjectID, Kind: "model.list"})
		if err != nil {
			return err
		}
		var page protocol.ModelPage
		if json.Unmarshal(result.Data, &page) != nil || page.Runtime == nil {
			return errors.New("Hub must expose its initialized model runtime before evaluation")
		}
		r.state.Runtime = page.Runtime
		if err = r.save(); err != nil {
			return err
		}
	}
	for index, model := range r.suite.Models {
		for _, test := range r.suite.Cases {
			id := fmt.Sprintf("%s_m%d_%s", r.state.ID, index, test.ID)
			position := slices.IndexFunc(r.state.Cases, func(item CaseRun) bool { return item.ID == id })
			if position < 0 {
				r.state.Cases = append(r.state.Cases, CaseRun{ID: id, Model: model, CaseID: test.ID, State: "pending"})
				position = len(r.state.Cases) - 1
				if err = r.save(); err != nil {
					return err
				}
			}
			caseCtx, cancel := context.WithTimeout(ctx, r.timeout)
			err = r.runCase(caseCtx, position, test)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *runner) command(ctx context.Context, command protocol.Command) (protocol.CommandResult, error) {
	attempt, exists := r.state.Commands[command.ID]
	var result protocol.CommandResult
	var err error
	if exists {
		if string(protocol.JSON(attempt.Command)) != string(protocol.JSON(command)) {
			return result, errors.New("evaluation command identity changed")
		}
		err = r.client.request(ctx, "GET", "/v1/commands/"+url.PathEscape(command.ID), nil, &result)
	} else {
		r.state.Commands[command.ID] = Attempt{Command: command}
		if err = r.save(); err != nil {
			return result, err
		}
		err = r.client.request(ctx, "POST", "/v1/commands", command, &result)
	}
	if err != nil {
		return result, fmt.Errorf("command %s has an unconfirmed receipt, query the original command before continuing %w", command.ID, err)
	}
	if result.CommandID != command.ID {
		return result, errors.New("evaluation received another command's receipt")
	}
	for result.State == "accepted" || result.State == "running" {
		if err = r.wait(ctx); err != nil {
			return result, fmt.Errorf("command %s remains unconfirmed %w", command.ID, err)
		}
		if err = r.client.request(ctx, "GET", "/v1/commands/"+url.PathEscape(command.ID), nil, &result); err != nil {
			return result, err
		}
		if result.CommandID != command.ID {
			return result, errors.New("evaluation received another command's receipt")
		}
	}
	r.state.Commands[command.ID] = Attempt{Command: command, Result: &result}
	if err = r.save(); err != nil {
		return result, err
	}
	if result.State != "completed" {
		return result, fmt.Errorf("command %s ended in %s, inspect its receipt", command.ID, result.State)
	}
	return result, nil
}

func (r *runner) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(r.interval):
		return nil
	}
}

func (r *runner) runCase(ctx context.Context, index int, test Case) error {
	item := &r.state.Cases[index]
	if item.State == "complete" {
		return r.archiveCase(ctx, item)
	}
	policy := test.Policy
	policy.Model, policy.ReasoningEffort = item.Model.ID, item.Model.ReasoningEffort
	if policy.AccessMode == "" {
		policy.AccessMode = "read-only"
	}
	if policy.Mode == "" {
		policy.Mode = "focused"
	}
	created, err := r.command(ctx, protocol.Command{ID: item.ID + "_create", DeviceID: r.state.Coordinator, ProjectID: r.suite.ProjectID, Kind: "create", Title: "Evaluation · " + test.ID, Policy: &policy})
	if err != nil {
		return err
	}
	if created.Session == nil || created.Session.ProjectID != r.suite.ProjectID {
		return errors.New("evaluation create returned another project")
	}
	item.SessionID, item.State = created.Session.ID, "running"
	if err = r.save(); err != nil {
		return err
	}
	startID := item.ID + "_start"
	if _, err = r.command(ctx, protocol.Command{ID: startID, DeviceID: r.state.Coordinator, SessionID: item.SessionID, Kind: "start", ExpectedPolicyVersion: created.Session.PolicyVersion, Input: []protocol.Input{{Type: "text", Text: test.Prompt}}}); err != nil {
		return err
	}
	var session protocol.Session
	for {
		var sessions []protocol.Session
		if err = r.client.request(ctx, "GET", "/v1/sessions", nil, &sessions); err != nil {
			return err
		}
		position := slices.IndexFunc(sessions, func(value protocol.Session) bool { return value.ID == item.SessionID })
		if position < 0 {
			return errors.New("evaluation conversation is no longer visible")
		}
		session = sessions[position]
		if session.LastStartID != startID {
			return errors.New("evaluation conversation received another input, inspect the original run")
		}
		if !session.ExecutionUnconfirmed() && session.State != "starting" {
			break
		}
		if session.State == "unknown" {
			return fmt.Errorf("evaluation conversation %s has an unknown execution outcome", session.ID)
		}
		if err = r.wait(ctx); err != nil {
			return fmt.Errorf("evaluation conversation %s is still %s, its original run remains recorded %w", session.ID, session.State, err)
		}
	}
	trace := Trace{Session: session, Events: []protocol.Event{}}
	path := "/v1/projects/" + url.PathEscape(r.suite.ProjectID) + "/harness/turn?sessionId=" + url.QueryEscape(session.ID)
	if err = r.client.request(ctx, "GET", path, nil, &trace.Context); err != nil {
		return err
	}
	var cursor int64
	traceBytes := 0
	for {
		var page []protocol.Event
		path = fmt.Sprintf("/v1/events?sessionId=%s&after=%d", url.QueryEscape(session.ID), cursor)
		if err = r.client.request(ctx, "GET", path, nil, &page); err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		traceBytes += len(protocol.JSON(page))
		if traceBytes > 64<<20 || len(trace.Events)+len(page) > 50000 {
			return errors.New("evaluation evidence exceeds the per-case trace limit, inspect the original conversation")
		}
		if page[len(page)-1].Sequence <= cursor {
			return errors.New("evaluation event cursor did not advance")
		}
		trace.Events = append(trace.Events, page...)
		cursor = page[len(page)-1].Sequence
		if len(page) < 500 {
			break
		}
	}
	item.Trace = item.ID + ".trace.json"
	if err = writeJSON(filepath.Join(r.output, item.Trace), trace); err != nil {
		return err
	}
	grade := GradeTrace(test, item.Model, r.state.Project, r.state.Harness, trace)
	item.Grade, item.State = &grade, "complete"
	if err = r.save(); err != nil {
		return err
	}
	return r.archiveCase(ctx, item)
}

func (r *runner) archiveCase(ctx context.Context, item *CaseRun) error {
	if item.Archived {
		return nil
	}
	var archived protocol.Session
	if err := r.client.request(ctx, "PATCH", "/v1/sessions/"+url.PathEscape(item.SessionID), map[string]bool{"archived": true}, &archived); err != nil {
		return err
	}
	if archived.ID != item.SessionID || !archived.Archived {
		return errors.New("evaluation conversation archive was not confirmed")
	}
	item.Archived = true
	return r.save()
}

func (r *runner) report(runErr error) Report {
	report := Report{Format: 1, RunID: r.state.ID, CreatedAt: r.state.CreatedAt, UpdatedAt: protocol.Now(), Suite: r.suite, SuiteHash: r.state.SuiteHash, HubDeviceID: r.state.HubDeviceID, Project: r.state.Project, Harness: r.state.Harness, Runtime: r.state.Runtime, Cases: r.state.Cases, Complete: len(r.state.Cases) == len(r.suite.Models)*len(r.suite.Cases), Passed: true}
	for _, item := range report.Cases {
		report.Complete = report.Complete && item.State == "complete" && item.Archived
		report.Passed = report.Passed && item.Grade != nil && item.Grade.Passed
	}
	report.Passed = report.Passed && report.Complete && runErr == nil
	if runErr != nil {
		report.Error = runErr.Error()
	}
	return report
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".evaluation-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(append(raw, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
