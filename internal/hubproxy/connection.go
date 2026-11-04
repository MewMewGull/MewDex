package hubproxy

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

type ConnectionTest struct {
	DurationMs int64 `json:"durationMs"`
}

// TestConnection authenticates with the same settings as the managed tunnel.
// The short-lived connection closes as soon as the handshake completes.
func TestConnection(ctx context.Context, settings Settings) (ConnectionTest, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	started := time.Now()
	client, _, err := connectSSH(ctx, settings)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ConnectionTest{}, errors.New("SSH 连接测试超时，请检查网络和服务器。")
		}
		return ConnectionTest{}, err
	}
	defer client.Close()
	return ConnectionTest{DurationMs: time.Since(started).Milliseconds()}, nil
}

func connectSSH(ctx context.Context, settings Settings) (*ssh.Client, net.Conn, error) {
	if err := settings.validateSSH(); err != nil {
		return nil, nil, err
	}
	auth, err := settings.authentication()
	if err != nil {
		return nil, nil, err
	}
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, errors.New("无法连接 SSH 服务器，请检查主机、端口和网络。")
	}
	closeOnCancel := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer closeOnCancel()
	preferKnownHostKeys(auth, address, raw.RemoteAddr())
	_ = raw.SetDeadline(time.Now().Add(15 * time.Second))
	conn, channels, requests, err := ssh.NewClientConn(raw, address, auth)
	if err != nil {
		_ = raw.Close()
		return nil, nil, errors.New("SSH 认证或主机密钥校验失败，请核对用户、私钥和 known_hosts。")
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return ssh.NewClient(conn, channels, requests), raw, nil
}
