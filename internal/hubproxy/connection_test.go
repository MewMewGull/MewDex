package hubproxy

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestConnectionAuthenticatesAndClosesWithoutOpeningForward(t *testing.T) {
	f := fixture(t)
	f.settings.RemoteBind = ""
	f.settings.RemotePort = 0
	result, err := TestConnection(context.Background(), f.settings)
	if err != nil || result.DurationMs < 0 {
		t.Fatalf("valid SSH connection failed: %+v %v", result, err)
	}
	connection := <-f.connections
	closed := make(chan error, 1)
	go func() { closed <- connection.Wait() }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("connection test left an SSH connection open")
	}
	select {
	case <-f.forwarded:
		t.Fatal("connection test requested a reverse listener")
	default:
	}
}

func TestConnectionRejectsUnknownHostAndWrongIdentity(t *testing.T) {
	for _, test := range []string{"unknown host", "wrong identity"} {
		t.Run(test, func(t *testing.T) {
			f := fixture(t)
			other, private := privateKey(t)
			path, content := f.settings.IdentityFile, private
			if test == "unknown host" {
				address := net.JoinHostPort(f.settings.Host, strconv.Itoa(f.settings.Port))
				path = f.settings.KnownHostsFile
				content = []byte(knownhosts.Line([]string{address}, other.PublicKey()))
			}
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := TestConnection(context.Background(), f.settings); err == nil {
				t.Fatal("connection test accepted an untrusted endpoint or identity")
			}
		})
	}
}

func TestConnectionHonorsDeadlineDuringHandshake(t *testing.T) {
	f := fixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			defer connection.Close()
			_, _ = io.Copy(io.Discard, connection)
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	f.settings.Port, _ = strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := TestConnection(ctx, f.settings); err == nil {
		t.Fatal("a stalled SSH handshake passed its connection test")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("connection test ignored the caller's deadline")
	}
}
