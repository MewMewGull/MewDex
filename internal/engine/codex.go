package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("Codex RPC %d: %s", e.Code, e.Message) }

type Codex struct {
	process       *childprocess.Process
	in            io.WriteCloser
	writeMu       sync.Mutex
	mu            sync.Mutex
	pending       map[string]chan rpcMessage
	next          atomic.Int64
	notifications chan Notification
	stream        *notificationStream
	home          string
	program       string
	arguments     []string
	done          chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

// Resolve avoids shell command construction. npm shims are executed through Node directly.
func Resolve(c config.Engine) (string, []string, error) {
	if c.Executable != "" {
		return c.Executable, append([]string{}, c.Args...), nil
	}
	if p, err := exec.LookPath("codex"); err == nil {
		if runtime.GOOS != "windows" || strings.HasSuffix(strings.ToLower(p), ".exe") {
			return p, nil, nil
		}
		js := filepath.Join(filepath.Dir(p), "node_modules", "@openai", "codex", "bin", "codex.js")
		if _, err = os.Stat(js); err == nil {
			node, e := exec.LookPath("node")
			return node, []string{js}, e
		}
	}
	if runtime.GOOS == "windows" {
		js := filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules", "@openai", "codex", "bin", "codex.js")
		if _, err := os.Stat(js); err == nil {
			node, e := exec.LookPath("node")
			return node, []string{js}, e
		}
	}
	return "", nil, errors.New("Codex not found; configure engine.executable and optional engine.args")
}

const CompatibleVersion = "0.162.1"

// StartOptions permits a trusted local caller to retain bounded engine
// diagnostics. No diagnostic destination is accepted from the network protocol.
type StartOptions struct{ Diagnostics io.Writer }

func Start(ctx context.Context, c config.Engine) (*Codex, error) {
	return StartWithOptions(ctx, c, StartOptions{})
}

func StartWithOptions(ctx context.Context, c config.Engine, options StartOptions) (*Codex, error) {
	exe, args, err := Resolve(c)
	if err != nil {
		return nil, err
	}
	version, versionErr := executableVersion(ctx, exe, args)
	if versionErr != nil {
		return nil, errors.New("cannot determine Codex engine version")
	}
	if version != "codex-cli "+CompatibleVersion {
		return nil, fmt.Errorf("this adapter requires Codex %s; configure a project-local engine without replacing the system installation", CompatibleVersion)
	}
	if runtime.GOOS == "windows" {
		// Native legacy tokens cannot provide the project's read allowlist.
		// Select MXC explicitly so unsupported hosts fail without a silent
		// legacy fallback or machine-wide provisioning.
		args = append(args, "-c", `windows.sandbox="mxc"`)
	}
	launchArguments := append([]string(nil), args...)
	args = append(args, "app-server", "--listen", "stdio://")
	cmd := exec.Command(exe, args...)
	cmd.Stderr = options.Diagnostics
	cmd.Env = independentEngineEnvironment(os.Environ())
	setenv := func(k, v string) {
		prefix := strings.ToUpper(k) + "="
		filtered := cmd.Env[:0]
		for _, entry := range cmd.Env {
			if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
				filtered = append(filtered, entry)
			}
		}
		cmd.Env = append(filtered, k+"="+v)
	}
	if shell, shellErr := nativeEngineShell(); shellErr != nil {
		return nil, shellErr
	} else if shell != "" {
		setenv("SHELL", shell)
		setenv("PATH", nativeEngineSearchPath(os.Getenv("PATH")))
	}
	if c.Home != "" {
		if err := os.MkdirAll(c.Home, 0700); err != nil {
			return nil, err
		}
		setenv("CODEX_HOME", c.Home)
	}
	if c.ProxyURL != "" {
		proxy, err := c.AuthenticatedProxyURL()
		if err != nil {
			return nil, err
		}
		setenv("HTTPS_PROXY", proxy)
		setenv("HTTP_PROXY", proxy)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, writer, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stdout = writer
	process, err := childprocess.StartContext(ctx, cmd)
	writer.Close()
	if err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	stream := newNotificationStream()
	home := c.Home
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".codex")
		}
	}
	e := &Codex{process: process, in: in, pending: map[string]chan rpcMessage{}, notifications: stream.out, stream: stream, home: home, program: cmd.Path, arguments: launchArguments, done: make(chan struct{})}
	go e.read(out)
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result json.RawMessage
	err = e.Call(initCtx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "mewdex", "version": "0.1.0-dev", "title": "MewDex"}, "capabilities": map[string]bool{"experimentalApi": true}}, &result)
	if err == nil {
		err = e.send(rpcMessage{Method: "initialized", Params: json.RawMessage(`{}`)})
	}
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("Codex initialize: %w", err)
	}
	return e, nil
}

// A desktop-launched service can inherit its hosting agent's tool pipe,
// permission profile and conversation identifiers. Those describe the host
// agent, not this independent engine. CODEX_HOME is handled explicitly above.
func independentEngineEnvironment(inherited []string) []string {
	result := make([]string, 0, len(inherited))
	for _, entry := range inherited {
		name, _, found := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if found && (strings.HasPrefix(upper, "CODEX_") && !strings.EqualFold(name, "CODEX_HOME") || strings.HasPrefix(upper, "BROWSER_USE_ACCESS_")) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func (e *Codex) send(m rpcMessage) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	select {
	case <-e.done:
		return errors.New("Codex disconnected")
	default:
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = e.in.Write(append(b, '\n'))
	return err
}
func (e *Codex) Call(ctx context.Context, method string, params any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := strconv.FormatInt(e.next.Add(1), 10)
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan rpcMessage, 1)
	e.mu.Lock()
	e.pending[id] = ch
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.pending, id); e.mu.Unlock() }()
	if err = e.send(rpcMessage{ID: json.RawMessage(id), Method: method, Params: b}); err != nil {
		return &UnknownOutcome{Cause: err}
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil {
			if err := json.Unmarshal(m.Result, out); err != nil {
				return &UnknownOutcome{Cause: err}
			}
		}
		return nil
	case <-ctx.Done():
		return &UnknownOutcome{Cause: ctx.Err()}
	case <-e.done:
		return &UnknownOutcome{Cause: errors.New("Codex disconnected")}
	}
}
func (e *Codex) Respond(ctx context.Context, id, result json.RawMessage) error {
	if !json.Valid(id) || !json.Valid(result) {
		return errors.New("invalid RPC response")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if err := e.send(rpcMessage{ID: id, Result: result}); err != nil {
			return &UnknownOutcome{Cause: err}
		}
		return nil
	}
}
func (e *Codex) Reject(ctx context.Context, id json.RawMessage, message string) error {
	if !json.Valid(id) {
		return errors.New("invalid RPC request identifier")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return e.send(rpcMessage{ID: id, Error: &rpcError{Code: -32601, Message: message}})
	}
}
func (e *Codex) Notifications() <-chan Notification { return e.notifications }
func (e *Codex) ModelRuntime() protocol.ModelRuntime {
	return protocol.ModelRuntime{Adapter: "codex-app-server", Version: CompatibleVersion, Transport: "jsonrpc-stdio"}
}
func (e *Codex) Done() <-chan struct{} { return e.done }
func (e *Codex) read(r io.ReadCloser) {
	defer r.Close()
	defer close(e.done)
	defer e.stream.finish()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		var m rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue
		}
		if m.Method != "" {
			if !e.stream.push(Notification{Method: m.Method, Params: m.Params, RequestID: m.ID}) {
				_ = e.process.Kill()
				break
			}
			continue
		}
		e.mu.Lock()
		ch := e.pending[string(m.ID)]
		e.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
	}
	_ = e.process.Kill()
	_ = e.process.Wait()
}
func (e *Codex) Close() error {
	e.closeOnce.Do(func() {
		_ = e.in.Close()
		select {
		case <-e.done:
		case <-time.After(3 * time.Second):
			e.closeErr = e.process.Kill()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				e.closeErr = errors.Join(e.closeErr, errors.New("engine process exit remains unconfirmed"))
			}
		}
		select {
		case <-e.done:
			e.closeErr = errors.Join(e.closeErr, e.process.Kill())
		default:
		}
	})
	return e.closeErr
}
