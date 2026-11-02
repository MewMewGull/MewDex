//go:build windows

// backhaul-risk uses a private loopback SSH stand-in and an isolated OS entry.
// It never reads a real SSH key or connects to an external host.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mewdex/internal/backhaul"
	"mewdex/internal/childprocess"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
)

func write(path string, value any) error { return os.WriteFile(path, protocol.JSON(value), 0600) }

func fakeSSH(args []string) error {
	values := map[string]string{}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-i" || args[i] == "-R" {
			values[args[i]] = args[i+1]
		}
	}
	if len(args) == 0 || args[len(args)-1] != "mewdex-fixture.invalid" || filepath.Base(values["-i"]) != "fixture-key" {
		return errors.New("not a private SSH stand-in invocation")
	}
	root := filepath.Dir(values["-i"])
	if raw, err := os.ReadFile(filepath.Join(root, "fixture.json")); err != nil || !strings.Contains(string(raw), "synthetic-loopback-only") {
		return errors.New("private fixture marker is missing")
	}
	parts := strings.Split(values["-R"], ":")
	if len(parts) != 4 || parts[0] != "127.0.0.1" || parts[2] != "127.0.0.1" {
		return errors.New("fixture requires two loopback endpoints")
	}
	listener, err := net.Listen("tcp", parts[0]+":"+parts[1])
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	controlDir := filepath.Join(root, "carrier")
	if err := os.MkdirAll(controlDir, 0700); err != nil {
		return err
	}
	control, err := lifecycle.Start(controlDir, "synthetic-ssh", cancel)
	if err != nil {
		return err
	}
	defer control.Close()
	control.Ready("")
	target, _ := url.Parse("http://" + parts[2] + ":" + parts[3])
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	<-ctx.Done()
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	return server.Shutdown(stop)
}

func wait(ctx context.Context, check func() bool) error {
	deadline := time.NewTimer(13 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("private backhaul observation timed out")
		case <-tick.C:
		}
	}
}

func health(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", base+"/health", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == 200
}

func worker(root string) (map[string]any, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, _ := backhaul.Status(ctx, root)
	control, _ := lifecycle.Read(filepath.Join(root, "data"))
	carrier, _ := lifecycle.Read(filepath.Join(root, "carrier"))
	return state, control.InstanceID, carrier.InstanceID
}

func leasesReleased(root string) bool {
	installation, err := lifecycle.Installation(root)
	if err != nil {
		return false
	}
	defer installation.Close()
	data, err := lifecycle.Acquire(filepath.Join(root, "data"))
	if err != nil {
		return false
	}
	data.Close()
	return true
}

func run(program, parent string) (resultErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 95*time.Second)
	defer cancel()
	root, err := os.MkdirTemp(parent, "owned backhaul ")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	backend, err := startup.Native()
	if err != nil {
		return err
	}
	controller, err := startup.New(root, backend)
	if err != nil {
		return err
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"MewDex","protocolVersion":1}`))
	}))
	defer target.Close()
	local, _ := url.Parse(target.URL)
	localPort, _ := strconv.Atoi(local.Port())
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	remotePort := reserve.Addr().(*net.TCPAddr).Port
	_ = reserve.Close()
	public := "http://127.0.0.1:" + strconv.Itoa(remotePort)
	self, _ := os.Executable()
	raw, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	c := backhaul.Config{ID: "synthetic-backhaul-" + protocol.ID(), SSHPath: self, SSHSHA256: hex.EncodeToString(digest[:]), Host: "mewdex-fixture.invalid", Port: 22, User: "synthetic", IdentityFile: filepath.Join(root, "fixture-key"), KnownHostsFile: filepath.Join(root, "known hosts"), RemoteBind: "127.0.0.1", RemotePort: remotePort, LocalPort: localPort, PublicURL: public}
	for _, name := range []string{c.IdentityFile, c.KnownHostsFile} {
		if err := os.WriteFile(name, []byte("synthetic metadata, not an SSH credential"), 0600); err != nil {
			return err
		}
	}
	if err := write(filepath.Join(root, "fixture.json"), map[string]any{"purpose": "synthetic-loopback-only", "startedAt": protocol.Now()}); err != nil {
		return err
	}
	if err := write(filepath.Join(root, "backhaul.json"), c); err != nil {
		return err
	}
	_, binding, err := backhaul.Read(root)
	if err != nil {
		return err
	}
	report := map[string]any{"root": root, "at": protocol.Now(), "state": "running", "realSSHKeysRead": false, "externalNetworkUsed": false, "modelInferenceRun": false, "productionServicesUpdated": false, "finalCandidate": false}
	if err := write(filepath.Join(root, "report.json"), report); err != nil {
		return err
	}
	fmt.Println("Private backhaul fixture:", root)
	var definition startup.Definition
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if record, err := lifecycle.Read(filepath.Join(root, "data")); err == nil {
			_, _ = lifecycle.Call(cleanup, record, true)
		}
		if definition.Owner != "" {
			if entry, err := backend.Inspect(cleanup, definition); err == nil && entry.Running {
				_ = backend.Stop(cleanup, definition)
			}
		}
		_, removeErr := controller.Apply(cleanup, false, "", startup.Binding{})
		leaseErr := wait(cleanup, func() bool { return leasesReleased(root) && !health(cleanup, public) })
		info, inspectErr := controller.Status(cleanup, binding)
		cleanupErr := errors.Join(removeErr, leaseErr, inspectErr)
		if info.Registered {
			cleanupErr = errors.Join(cleanupErr, errors.New("private OS entry remains registered"))
		}
		resultErr = errors.Join(resultErr, cleanupErr)
		report["cleanupConfirmed"], report["osEntryRemoved"], report["sameProgramSentinelPreserved"] = cleanupErr == nil, !info.Registered, health(cleanup, target.URL)
		report["state"] = "passed"
		if resultErr != nil {
			report["state"], report["error"] = "failed-or-unknown", resultErr.Error()
		}
		_ = write(filepath.Join(root, "report.json"), report)
		encoded, _ := json.Marshal(report)
		fmt.Println(string(encoded))
	}()
	call := func(mode string) ([]byte, error) {
		command := exec.CommandContext(ctx, program, mode, "-root", root)
		return childprocess.Output(command, 1<<20)
	}
	if _, err = call("enable"); err != nil {
		return err
	}
	var registration startup.Record
	stored, err := os.ReadFile(filepath.Join(root, "startup.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(stored, &registration); err != nil {
		return err
	}
	definition = registration.Definition
	info, err := controller.Status(ctx, binding)
	if err != nil || !info.Enabled || info.Running {
		return errors.New("private startup registration was not inactive and matching")
	}
	report["registeredWithoutStarting"] = true
	if _, err = call("start"); err != nil {
		return err
	}
	var firstWorker, firstCarrier string
	if err = wait(ctx, func() bool {
		state, w, k := worker(root)
		if state["connected"] == true && health(ctx, public) {
			firstWorker, firstCarrier = w, k
			return w != "" && k != ""
		}
		return false
	}); err != nil {
		return err
	}
	report["startedThroughOSManager"] = true
	if _, err = call("serve"); err == nil {
		return errors.New("a second supervisor bypassed the installation lease")
	}
	if _, current, _ := worker(root); current != firstWorker {
		return errors.New("singleton refusal changed the existing worker")
	}
	report["secondInstanceRefused"] = true
	carrierRecord, err := lifecycle.Read(filepath.Join(root, "carrier"))
	if err != nil {
		return err
	}
	if _, err = lifecycle.Call(ctx, carrierRecord, true); err != nil {
		return err
	}
	if err = wait(ctx, func() bool {
		state, w, k := worker(root)
		return state["connected"] == true && w == firstWorker && k != "" && k != firstCarrier && health(ctx, public)
	}); err != nil {
		return err
	}
	report["carrierExitReconnectedWithoutRestartingWorker"] = true
	if err = backend.Stop(ctx, definition); err != nil {
		return err
	}
	if err = wait(ctx, func() bool { return leasesReleased(root) && !health(ctx, public) }); err != nil {
		return err
	}
	if !health(ctx, target.URL) {
		return errors.New("same-program sentinel was terminated")
	}
	report["forcedOwnerExitClosedOwnedCarrier"] = true
	if _, err = call("start"); err != nil {
		return err
	}
	var secondWorker string
	if err = wait(ctx, func() bool {
		state, w, _ := worker(root)
		if state["connected"] == true && w != "" && w != firstWorker && health(ctx, public) {
			secondWorker = w
			return true
		}
		return false
	}); err != nil {
		return err
	}
	report["freshWorkerAfterForcedExit"] = secondWorker != ""
	c.ID += "-changed"
	if err = write(filepath.Join(root, "backhaul.json"), c); err != nil {
		return err
	}
	if err = wait(ctx, func() bool {
		state, _, _ := worker(root)
		last, _ := state["lastRecorded"].(backhaul.State)
		return last.State == "suspended" && state["workerReachable"] == false && leasesReleased(root) && !health(ctx, public)
	}); err != nil {
		return err
	}
	if _, err = call("start"); err == nil {
		return errors.New("changed profile was automatically adopted")
	}
	report["profileChangeSuspendedAndRefusedRestart"] = true
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-F" {
		if err := fakeSSH(os.Args[1:]); err != nil {
			os.Exit(1)
		}
		return
	}
	program := flag.String("program", "", "development backhaul executable")
	parent := flag.String("parent", "", "existing private evidence directory")
	flag.Parse()
	if *program == "" || *parent == "" {
		fmt.Fprintln(os.Stderr, "explicit program and private parent are required")
		os.Exit(1)
	}
	if err := run(*program, *parent); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
