package hubproxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type sshFixture struct {
	settings    Settings
	forwarded   chan net.Listener
	connections chan *ssh.ServerConn
}

func privateKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(block)
}

func fixture(t *testing.T) sshFixture {
	t.Helper()
	root := t.TempDir()
	hostPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	alternative, _ := privateKey(t)
	identity, raw := privateKey(t)
	key := filepath.Join(root, "专用 SSH key")
	hosts := filepath.Join(root, "known hosts")
	if err := os.WriteFile(key, raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); _ = listener.Close(); workers.Wait() })
	if err := os.WriteFile(hosts, []byte(knownhosts.Line([]string{listener.Addr().String()}, host.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	f := sshFixture{Settings{true, "127.0.0.1", n, "fixture", key, hosts, "127.0.0.1", 47834, "https://127.0.0.1:1"}, make(chan net.Listener, 8), make(chan *ssh.ServerConn, 8)}
	server := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
		if string(offered.Marshal()) != string(identity.PublicKey().Marshal()) {
			return nil, fmt.Errorf("unknown key")
		}
		return nil, nil
	}}
	server.AddHostKey(host)
	server.AddHostKey(alternative) // The preferred default is intentionally not in known_hosts.
	workers.Go(func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer raw.Close()
				stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
				defer stop()
				conn, channels, requests, err := ssh.NewServerConn(raw, server)
				if err != nil {
					return
				}
				defer conn.Close()
				f.connections <- conn
				go func() {
					for channel := range channels {
						_ = channel.Reject(ssh.Prohibited, "no sessions")
					}
				}()
				var remote net.Listener
				defer func() {
					if remote != nil {
						_ = remote.Close()
					}
				}()
				for request := range requests {
					switch request.Type {
					case "tcpip-forward":
						var payload struct {
							Address string
							Port    uint32
						}
						if ssh.Unmarshal(request.Payload, &payload) != nil || remote != nil {
							_ = request.Reply(false, nil)
							continue
						}
						remote, err = net.Listen("tcp", "127.0.0.1:0")
						if err != nil {
							_ = request.Reply(false, nil)
							continue
						}
						_ = request.Reply(true, nil)
						f.forwarded <- remote
						forwardListener := remote
						workers.Go(func() {
							for {
								stream, err := forwardListener.Accept()
								if err != nil {
									return
								}
								workers.Go(func() {
									defer stream.Close()
									channel, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(struct {
										Address    string
										Port       uint32
										Origin     string
										OriginPort uint32
									}{payload.Address, payload.Port, "127.0.0.1", 12345}))
									if err != nil {
										return
									}
									defer channel.Close()
									go ssh.DiscardRequests(reqs)
									done := make(chan struct{})
									go func() { _, _ = io.Copy(stream, channel); _ = stream.Close(); close(done) }()
									_, _ = io.Copy(channel, stream)
									_ = channel.Close()
									_ = stream.Close()
									<-done
								})
							}
						})
					case "cancel-tcpip-forward":
						if remote != nil {
							_ = remote.Close()
						}
						_ = request.Reply(true, nil)
					default:
						_ = request.Reply(true, nil)
					}
				}
			})
		}
	})
	return f
}

func waitForward(t *testing.T, f sshFixture) net.Listener {
	t.Helper()
	select {
	case listener := <-f.forwarded:
		return listener
	case <-time.After(8 * time.Second):
		t.Fatal("Hub did not establish SSH reverse forwarding")
		return nil
	}
}

func TestInProcessTunnelReconnectsDisablesAndClosesOpenStreams(t *testing.T) {
	f := fixture(t)
	root := t.TempDir()
	data := filepath.Join(root, "data")
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	accepted := make(chan net.Conn, 8)
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			accepted <- c
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	if err := Save(root, f.settings); err != nil {
		t.Fatal(err)
	}
	closeHub := Start(context.Background(), root, data, target.Addr().String(), "fixture-instance")
	t.Cleanup(closeHub)
	remote := waitForward(t, f)
	stream, err := net.DialTimeout("tcp", remote.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = stream.Write([]byte("hello"))
	got := make([]byte, 5)
	if _, err = io.ReadFull(stream, got); err != nil || string(got) != "hello" {
		t.Fatalf("reverse stream failed: %q %v", got, err)
	}
	local := <-accepted
	defer local.Close()
	connection := <-f.connections
	_ = connection.Close()
	remote = waitForward(t, f)
	stream2, err := net.DialTimeout("tcp", remote.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream2.Close()
	_, _ = stream2.Write([]byte("again"))
	_ = stream2.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = io.ReadFull(stream2, got); err != nil || string(got) != "again" {
		t.Fatalf("reconnected stream failed: %q %v", got, err)
	}
	local2 := <-accepted
	defer local2.Close()
	f.settings.Enabled = false
	if err := Save(root, f.settings); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := ReadState(data)
		if err == nil && state.State == "disabled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disabled tunnel did not join: %+v %v", state, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = stream2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := stream2.Read(got); err == nil {
		t.Fatal("disabled tunnel kept a remote stream alive")
	}
	saved, _, err := Read(root)
	if err != nil || saved.Enabled || saved.Host != f.settings.Host || saved.IdentityFile != f.settings.IdentityFile {
		t.Fatal("disabling lost saved configuration")
	}
	// Starting a new Hub adopts saved enabled settings without a GUI request.
	f.settings.Enabled = true
	if err := Save(root, f.settings); err != nil {
		t.Fatal(err)
	}
	_ = waitForward(t, f)
	closed := make(chan struct{})
	go func() { closeHub(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("Hub shutdown retained SSH workers")
	}
}

func TestStrictHostKeysAndDisableWithMissingCredentials(t *testing.T) {
	f := fixture(t)
	other, _ := privateKey(t)
	address := net.JoinHostPort(f.settings.Host, strconv.Itoa(f.settings.Port))
	if err := os.WriteFile(f.settings.KnownHostsFile, []byte(knownhosts.Line([]string{address}, other.PublicKey())), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tunnel(ctx, f.settings, "127.0.0.1:1", func(string, string) {}); err == nil {
		t.Fatal("untrusted SSH host was accepted")
	}
	select {
	case <-f.forwarded:
		t.Fatal("untrusted host received forwarding")
	default:
	}
	if err := os.Remove(f.settings.IdentityFile); err != nil {
		t.Fatal(err)
	}
	if err := Save(t.TempDir(), f.settings); err == nil {
		t.Fatal("enabled missing credentials were accepted")
	}
	f.settings.Enabled = false
	if err := Save(t.TempDir(), f.settings); err != nil {
		t.Fatal("could not disable after credentials disappeared", err)
	}
}
