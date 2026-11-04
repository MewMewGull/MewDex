package hubproxy

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mewdex/internal/protocol"
)

type State struct {
	InstanceID  string `json:"instanceId"`
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Message     string `json:"message,omitempty"`
	UpdatedAt   string `json:"updatedAt"`
}

func ReadState(data string) (State, error) {
	var s State
	_, err := readJSON(filepath.Join(data, "hub-proxy-state.json"), &s)
	return s, err
}

// Start shares the Hub lifetime. Closing it joins all tunnels and forwarded
// streams, so another Hub can never inherit a connection from this instance.
func Start(parent context.Context, root, data, target, instance string) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() { defer close(done); run(ctx, root, data, target, instance) }()
	return func() { cancel(); <-done }
}

func run(ctx context.Context, root, data, target, instance string) {
	var cancel context.CancelFunc
	var done chan struct{}
	last := "unread"
	stop := func() {
		if cancel != nil {
			cancel()
			<-done
			cancel = nil
		}
	}
	defer stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		s, fingerprint, err := Read(root)
		key := fingerprint
		if err != nil {
			key = "invalid:" + err.Error()
		}
		if key != last {
			stop()
			last = key
			publish := func(state, message string) {
				_ = atomicJSON(data, "hub-proxy-state.json", State{instance, fingerprint, state, message, protocol.Now()})
			}
			switch {
			case err != nil:
				publish("error", "无法读取代理设置，请重新保存。")
			case !s.Enabled:
				publish("disabled", "")
			default:
				worker, stopWorker := context.WithCancel(ctx)
				cancel = stopWorker
				done = make(chan struct{})
				go func() { defer close(done); supervise(worker, s, target, publish) }()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func supervise(ctx context.Context, s Settings, target string, publish func(string, string)) {
	defer publish("stopped", "")
	backoff := time.Second
	for ctx.Err() == nil {
		publish("connecting", "正在连接 SSH 服务器…")
		started := time.Now()
		err := tunnel(ctx, s, target, publish)
		if ctx.Err() != nil {
			return
		}
		message := "连接中断，正在自动重连。"
		if err != nil {
			message = err.Error()
		}
		publish("reconnecting", message)
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func tunnel(ctx context.Context, s Settings, target string, publish func(string, string)) error {
	if err := s.Validate(); err != nil {
		return err
	}
	client, raw, err := connectSSH(ctx, s)
	if err != nil {
		return err
	}
	defer client.Close()
	closeOnCancel := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer closeOnCancel()
	listener, err := client.Listen("tcp", net.JoinHostPort(s.RemoteBind, strconv.Itoa(s.RemotePort)))
	if err != nil {
		return errors.New("服务器拒绝反向监听，请检查端口占用和 SSH 转发权限。")
	}
	_ = raw.SetDeadline(time.Time{})
	defer listener.Close()
	session, stop := context.WithCancel(ctx)
	defer stop()
	var workers sync.WaitGroup
	defer func() { stop(); _ = client.Close(); _ = listener.Close(); workers.Wait() }()
	results := make(chan error, 2)
	workers.Go(func() { results <- forward(session, listener, target) })
	workers.Go(func() {
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-session.Done():
				return
			case <-timer.C:
				_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					results <- errors.New("SSH 保活超时，将自动重连。")
					return
				}
				_ = raw.SetDeadline(time.Time{})
			}
		}
	})
	check := time.NewTicker(5 * time.Second)
	defer check.Stop()
	for {
		publish("connected", "")
		select {
		case <-ctx.Done():
			return nil
		case err := <-results:
			return err
		case <-check.C:
		}
	}
}

// OpenSSH prefers the host-key algorithms already recorded for the endpoint.
// Go's default ordering is independent of known_hosts; with several server
// keys it can otherwise select an unrecorded key and reject a trusted server.
// Query the verifier locally using a sentinel key, then retain its strict
// callback for the actual handshake. No newly offered key is ever trusted.
func preferKnownHostKeys(auth *ssh.ClientConfig, address string, remote net.Addr) {
	probe, _ := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	var mismatch *knownhosts.KeyError
	if err := auth.HostKeyCallback(address, remote, probe); !errors.As(err, &mismatch) || len(mismatch.Want) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, want := range mismatch.Want {
		algorithms := []string{want.Key.Type()}
		if want.Key.Type() == ssh.KeyAlgoRSA {
			algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
		}
		for _, algorithm := range algorithms {
			if !seen[algorithm] {
				seen[algorithm] = true
				auth.HostKeyAlgorithms = append(auth.HostKeyAlgorithms, algorithm)
			}
		}
	}
}

func forward(ctx context.Context, listener net.Listener, target string) error {
	ctx, cancel := context.WithCancel(ctx)
	var streams sync.WaitGroup
	defer func() { cancel(); streams.Wait() }()
	for {
		remote, err := listener.Accept()
		if err != nil {
			return err
		}
		streams.Go(func() {
			defer remote.Close()
			local, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", target)
			if err != nil {
				return
			}
			defer local.Close()
			closeStreams := context.AfterFunc(ctx, func() { _ = remote.Close(); _ = local.Close() })
			defer closeStreams()
			copied := make(chan struct{})
			go func() { _, _ = io.Copy(remote, local); _ = remote.Close(); close(copied) }()
			_, _ = io.Copy(local, remote)
			_ = local.Close()
			_ = remote.Close()
			<-copied
		})
	}
}
