package backhaul

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
)

var ErrProfileChanged = errors.New("backhaul profile changed; review and enable the updated configuration")

type State struct {
	ID          string `json:"id"`
	InstanceID  string `json:"instanceId"`
	ProfileHash string `json:"profileHash"`
	State       string `json:"state"`
	Attempt     uint64 `json:"attempt"`
	LastIssue   string `json:"lastIssue,omitempty"`
	RetryAt     string `json:"retryAt,omitempty"`
	UpdatedAt   string `json:"updatedAt"`
}

type carrier interface {
	Done() <-chan struct{}
	Wait() error
	Kill() error
}

type supervisor struct {
	config         Config
	binding        startup.Binding
	state          State
	save           func(State) error
	checkCurrent   func() error
	probe          func(context.Context, string) error
	launch         func(context.Context) (carrier, error)
	interval       time.Duration
	minimumBackoff time.Duration
	maximumBackoff time.Duration
}

func newSupervisor(root string, c Config, binding startup.Binding, save func(State) error) *supervisor {
	r := &supervisor{config: c, binding: binding, save: save, probe: probe, interval: 5 * time.Second, minimumBackoff: time.Second, maximumBackoff: 30 * time.Second}
	r.state.ID, r.state.ProfileHash = c.ID, binding.ProfileSHA256
	r.checkCurrent = func() error {
		_, now, err := Read(root)
		if err != nil || now != binding {
			return ErrProfileChanged
		}
		return nil
	}
	r.launch = func(ctx context.Context) (carrier, error) {
		if err := c.VerifySSH(); err != nil {
			return nil, err
		}
		command := exec.Command(c.SSHPath, c.sshArguments()...)
		command.Dir, command.Stdout, command.Stderr = root, io.Discard, io.Discard
		return childprocess.StartContext(ctx, command)
	}
	return r
}

func (r *supervisor) publish(state, issue string, retry time.Duration) error {
	r.state.State, r.state.LastIssue, r.state.UpdatedAt, r.state.RetryAt = state, issue, protocol.Now(), ""
	if retry > 0 {
		r.state.RetryAt = time.Now().Add(retry).UTC().Format(time.RFC3339Nano)
	}
	return r.save(r.state)
}

func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *supervisor) run(ctx context.Context) (err error) {
	defer func() {
		if errors.Is(err, ErrProfileChanged) {
			err = errors.Join(err, r.publish("suspended", "配置已变化，请重新审阅并启用。", 0))
		} else {
			err = errors.Join(err, r.publish("stopped", "", 0))
		}
	}()
	backoff := r.minimumBackoff
	for ctx.Err() == nil {
		if err := r.checkCurrent(); err != nil {
			return err
		}
		if err := r.probe(ctx, r.config.localURL()); err != nil {
			if err = r.publish("waiting-local", "等待本机 Hub 就绪。", r.interval); err != nil {
				return err
			}
			if !sleep(ctx, r.interval) {
				return nil
			}
			continue
		}
		r.state.Attempt++
		if err := r.publish("connecting", "", 0); err != nil {
			return err
		}
		process, launchErr := r.launch(ctx)
		connected := false
		if launchErr == nil {
			connected, err = r.observe(ctx, process)
			if err != nil || ctx.Err() != nil {
				return err
			}
		}
		if connected {
			backoff = r.minimumBackoff
		}
		if err = r.publish("retrying", "SSH 回程已断开或未能建立，稍后自动重试。", backoff); err != nil {
			return err
		}
		if !sleep(ctx, backoff) {
			return nil
		}
		backoff *= 2
		if backoff > r.maximumBackoff {
			backoff = r.maximumBackoff
		}
	}
	return nil
}

func (r *supervisor) observe(ctx context.Context, process carrier) (connected bool, err error) {
	defer func() {
		killErr := process.Kill()
		_ = process.Wait() // Process closure precedes any retry or lease release.
		err = errors.Join(err, killErr)
	}()
	for {
		select {
		case <-ctx.Done():
			return connected, nil
		case <-process.Done():
			return connected, nil
		default:
		}
		if err := r.checkCurrent(); err != nil {
			return connected, err
		}
		state, issue := "connected", ""
		if err := r.probe(ctx, r.config.localURL()); err != nil {
			state, issue = "waiting-local", "等待本机 Hub 恢复，保留现有 SSH 回程。"
		} else if err := r.probe(ctx, r.config.PublicURL); err != nil {
			state, issue = "verifying", "公网入口尚未通过健康检查。"
		}
		select {
		case <-process.Done():
			return connected, nil
		default:
		}
		connected = connected || state == "connected"
		if err := r.publish(state, issue, 0); err != nil {
			return connected, err
		}
		timer := time.NewTimer(r.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return connected, nil
		case <-process.Done():
			timer.Stop()
			return connected, nil
		case <-timer.C:
		}
	}
}

func probe(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var health struct {
		Name    string `json:"name"`
		Version int    `json:"protocolVersion"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) != nil || health.Name != "MewDex" || health.Version != protocol.Version {
		return errors.New("MewDex ingress health could not be verified")
	}
	return nil
}
