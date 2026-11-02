//go:build darwin

// service-guard-risk checks bounded service ownership, including watcher loss.
// Every process is created by this probe; no account or model is used.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"mewdex/internal/childprocess"
	"mewdex/internal/lifecycle"
	"mewdex/internal/processidentity"
	"mewdex/internal/serviceguard"
)

type ready struct {
	Owner   processidentity.Identity `json:"owner"`
	Child   processidentity.Identity `json:"child"`
	Watcher processidentity.Identity `json:"watcher"`
}

// Fault injection only: the production signal API intentionally remains
// limited to termination. The kernel still validates this probe's audit token
// when pausing/resuming its own freshly observed watcher.
func pauseSignal(expected processidentity.Identity, signal syscall.Signal) error {
	if signal != syscall.SIGSTOP && signal != syscall.SIGCONT {
		return errors.New("invalid fault signal")
	}
	current, err := processidentity.Lookup(expected.PID)
	if err != nil {
		return err
	}
	if !current.SameLifetime(expected) || current.PIDVersion != expected.PIDVersion || current.UID != uint32(os.Geteuid()) {
		return processidentity.ErrChanged
	}
	library, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return err
	}
	defer purego.Dlclose(library)
	symbol, err := purego.Dlsym(library, "proc_signal_with_audittoken")
	if err != nil {
		return err
	}
	var call func(unsafe.Pointer, int32) int32
	purego.RegisterFunc(&call, symbol)
	var token [8]uint32
	token[5], token[7] = uint32(current.PID), current.PIDVersion
	result := call(unsafe.Pointer(&token), int32(signal))
	runtime.KeepAlive(token)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}

func save(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}

func wait(timeout time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("bounded fixture observation timed out")
}

func held(root, role string) bool {
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err == nil {
		lease.Close()
	}
	return err != nil
}

func fixture(root, role string) error {
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err != nil {
		return err
	}
	defer lease.Close()
	if role == "hop" {
		cmd := exec.Command(os.Args[0], "fixture", root, "orphan")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd.Start() // Exit before waiting on the detached second fork.
	}
	if role == "payload" {
		for _, child := range []string{"child", "detached"} {
			cmd := exec.Command(os.Args[0], "fixture", root, child)
			if child == "detached" {
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			}
			if err = cmd.Start(); err != nil {
				return err
			}
			go func() { _ = cmd.Wait() }()
		}
	} else if role == "child" {
		cmd := exec.Command(os.Args[0], "fixture", root, "grandchild")
		if err = cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
	}
	identity, err := processidentity.Lookup(os.Getpid())
	if err != nil {
		return err
	}
	if err = save(filepath.Join(root, role+".json"), identity); err != nil {
		return err
	}
	deadline := time.Now().Add(fixtureTimeout(root))
	forked := false
	churned := false
	for time.Now().Before(deadline) {
		if role == "payload" && !churned {
			if _, err := os.Stat(filepath.Join(root, "start-concurrent-churn")); err == nil {
				if err := concurrentChurn(root); err != nil {
					return err
				}
				churned = true
			}
		}
		if role == "payload" && !churned {
			if _, err := os.Stat(filepath.Join(root, "start-short-churn")); err == nil {
				for i := 0; i < 96; i++ {
					if err := exec.Command("/usr/bin/true").Run(); err != nil {
						return err
					}
				}
				if err := os.WriteFile(filepath.Join(root, "short-churn.done"), []byte("96"), 0600); err != nil {
					return err
				}
				churned = true
			}
		}
		if role == "payload" && !forked {
			if _, err := os.Stat(filepath.Join(root, "start-fast-fork")); err == nil {
				cmd := exec.Command(os.Args[0], "fixture", root, "hop")
				if err = cmd.Start(); err != nil {
					return err
				}
				go func() { _ = cmd.Wait() }()
				forked = true
			}
		}
		if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func service(root string) error {
	installation, err := lifecycle.Installation(root)
	if err != nil {
		return err
	}
	defer installation.Close()
	ctx, cancel := context.WithTimeout(context.Background(), fixtureTimeout(root))
	defer cancel()
	return serviceguard.Run(ctx, filepath.Join(root, "data"), installation, true, func(ctx context.Context) error {
		command := exec.Command(os.Args[0], "fixture", root, "payload")
		child, err := childprocess.StartContext(ctx, command)
		if err != nil {
			return err
		}
		defer func() { _ = child.Kill(); _ = child.Wait() }()
		if err = wait(3*time.Second, func() bool {
			for _, role := range []string{"payload", "child", "grandchild", "detached"} {
				if !held(root, role) {
					return false
				}
			}
			return true
		}); err != nil {
			return err
		}
		owner, err := processidentity.Lookup(os.Getpid())
		if err != nil {
			return err
		}
		payload, err := processidentity.Lookup(command.Process.Pid)
		if err != nil {
			return err
		}
		pids, err := processidentity.ListPIDs()
		if err != nil {
			return err
		}
		var watcher processidentity.Identity
		for _, pid := range pids {
			candidate, readErr := processidentity.Lookup(pid)
			if readErr == nil && candidate.ParentUniqueID == owner.UniqueID && !candidate.SameLifetime(payload) {
				if watcher.PID != 0 {
					return errors.New("ambiguous fixture watcher")
				}
				watcher = candidate
			}
		}
		if watcher.PID == 0 {
			return errors.New("independent watcher was not found")
		}
		if err = save(filepath.Join(root, "ready.json"), ready{Owner: owner, Child: payload, Watcher: watcher}); err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
				if _, err := os.Stat(filepath.Join(root, "finish")); err == nil {
					return nil
				}
			}
		}
	})
}

func runCase(parent, mode string) (result map[string]any, resultErr error) {
	root := filepath.Join(parent, mode)
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, err
	}
	result = map[string]any{"root": root, "mode": mode, "packagingValidation": false}
	command := exec.Command(os.Args[0], "service", root)
	log, err := os.OpenFile(filepath.Join(root, "service.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err = command.Start(); err != nil {
		return result, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = os.WriteFile(filepath.Join(root, "stop"), nil, 0600)
		_ = os.WriteFile(filepath.Join(root, "finish"), nil, 0600)
		result["probeCleanupConfirmed"] = wait(3*time.Second, func() bool {
			for _, role := range []string{"payload", "child", "grandchild", "detached", "unrelated", "hop", "orphan"} {
				if held(root, role) {
					return false
				}
			}
			return true
		}) == nil
		result["error"] = fmt.Sprint(resultErr)
		_ = save(filepath.Join(root, "result.json"), result)
	}()
	var active ready
	if err = wait(6*time.Second, func() bool {
		raw, err := os.ReadFile(filepath.Join(root, "ready.json"))
		return err == nil && json.Unmarshal(raw, &active) == nil && active.Watcher.PID > 1
	}); err != nil {
		return result, err
	}
	result["identities"] = active
	unrelated := exec.Command(os.Args[0], "fixture", root, "unrelated")
	if err = unrelated.Start(); err != nil {
		return result, err
	}
	defer func() { _ = os.WriteFile(filepath.Join(root, "stop"), nil, 0600); _ = unrelated.Wait() }()
	if err = wait(2*time.Second, func() bool { return held(root, "unrelated") }); err != nil {
		return result, err
	}
	// Give event/snapshot ownership time to record all bounded long-lived roles.
	time.Sleep(200 * time.Millisecond)
	switch mode {
	case "normal":
		err = os.WriteFile(filepath.Join(root, "finish"), nil, 0600)
	case "owner-killed":
		err = processidentity.Signal(active.Owner, syscall.SIGKILL)
	case "watcher-killed":
		err = processidentity.Signal(active.Watcher, syscall.SIGKILL)
	case "short-churn":
		err = os.WriteFile(filepath.Join(root, "start-short-churn"), nil, 0600)
		if err == nil {
			err = wait(4*time.Second, func() bool { _, err := os.Stat(filepath.Join(root, "short-churn.done")); return err == nil })
		}
		if err == nil {
			select {
			case exitErr := <-done:
				return result, fmt.Errorf("monitor stopped a service with completed short tools: %v", exitErr)
			case <-time.After(1200 * time.Millisecond):
				result["shortToolsCompletedWithoutFalseFreeze"] = 96
			}
			err = os.WriteFile(filepath.Join(root, "finish"), nil, 0600)
		}
	case "concurrent-churn":
		err = os.WriteFile(filepath.Join(root, "start-concurrent-churn"), nil, 0600)
		var load loadResult
		var exited bool
		if err == nil {
			err = wait(70*time.Second, func() bool {
				select {
				case exitErr := <-done:
					exited = true
					result["prematureExit"] = fmt.Sprint(exitErr)
					return true
				default:
				}
				raw, readErr := os.ReadFile(filepath.Join(root, "concurrent-churn.done.json"))
				return readErr == nil && json.Unmarshal(raw, &load) == nil && load.Completed == loadWaves*loadWorkers*loadPerWorker
			})
		}
		if exited {
			return result, errors.New("monitor stopped during concurrent tool load")
		}
		if err == nil {
			select {
			case exitErr := <-done:
				return result, fmt.Errorf("monitor stopped after concurrent tools completed: %v", exitErr)
			case <-time.After(5 * time.Second):
			}
			current, readErr := processidentity.Lookup(active.Owner.PID)
			if readErr != nil || !current.SameLifetime(active.Owner) {
				return result, errors.New("service lifetime ended during the retention grace check")
			}
			result["concurrentLoad"] = load
			result["ownerAliveAfterRetentionGrace"] = true
			err = os.WriteFile(filepath.Join(root, "finish"), nil, 0600)
		}
	case "fast-fork":
		// An independent bounded helper resumes the watcher even if the observer
		// fails after SIGSTOP. Neither process uses a cached PID-only signal.
		raw, _ := json.Marshal(active.Watcher)
		resume := exec.Command(os.Args[0], "resume", string(raw))
		if err = resume.Start(); err != nil {
			break
		}
		defer func() { _ = pauseSignal(active.Watcher, syscall.SIGCONT); _ = resume.Wait() }()
		if err = pauseSignal(active.Watcher, syscall.SIGSTOP); err != nil {
			break
		}
		result["watcherPausedDuringFastIntermediate"] = true
		err = os.WriteFile(filepath.Join(root, "start-fast-fork"), nil, 0600)
		if err == nil {
			err = wait(2*time.Second, func() bool {
				raw, err := os.ReadFile(filepath.Join(root, "orphan.json"))
				var orphan processidentity.Identity
				if err != nil || json.Unmarshal(raw, &orphan) != nil {
					return false
				}
				current, err := processidentity.Lookup(orphan.PID)
				return err == nil && current.SameLifetime(orphan) && current.ParentPID == 1 && !held(root, "hop")
			})
		}
		if err == nil {
			err = pauseSignal(active.Watcher, syscall.SIGCONT)
		}
	}
	if err != nil {
		return result, err
	}
	select {
	case exitErr := <-done:
		result["ownerExit"] = fmt.Sprint(exitErr)
	case <-time.After(10 * time.Second):
		return result, errors.New("service did not finish its bounded ownership flow")
	}
	result["unrelatedPreserved"] = held(root, "unrelated")
	if mode == "fast-fork" {
		var report map[string]any
		if err := wait(3*time.Second, func() bool {
			raw, err := os.ReadFile(filepath.Join(root, "data", "service-ownership.report.json"))
			return err == nil && json.Unmarshal(raw, &report) == nil
		}); err != nil {
			return result, err
		}
		result["cleanupReport"] = report
		if report["observedCleanupConfirmed"] == true {
			if held(root, "orphan") {
				return result, errors.New("missing fast ancestor was falsely reported stopped")
			}
			result["fastAncestryObservedAndCleaned"] = true
		} else {
			for _, dir := range []string{"data", "installation"} {
				lease, err := lifecycle.Acquire(filepath.Join(root, dir))
				if lease != nil {
					lease.Close()
				}
				if !errors.Is(err, lifecycle.ErrUnconfirmed) {
					return result, errors.New("unknown fast ancestry did not freeze the service")
				}
			}
			result["uncertainAncestryRefused"] = true
		}
		if result["unrelatedPreserved"] != true {
			return result, errors.New("unrelated sentinel was stopped")
		}
		return result, nil
	}
	if mode == "watcher-killed" {
		freeze := func(dir string) bool {
			lock, err := lifecycle.Acquire(dir)
			if lock != nil {
				lock.Close()
			}
			return errors.Is(err, lifecycle.ErrUnconfirmed)
		}
		result["dataFrozen"] = freeze(filepath.Join(root, "data"))
		result["installationFrozen"] = freeze(filepath.Join(root, "installation"))
		if result["dataFrozen"] != true || result["installationFrozen"] != true {
			return result, errors.New("watcher loss allowed unconfirmed data adoption")
		}
	} else {
		if err = wait(3*time.Second, func() bool {
			for _, role := range []string{"payload", "child", "grandchild", "detached"} {
				if held(root, role) {
					return false
				}
			}
			return true
		}); err != nil {
			return result, err
		}
		result["ownedLeasesReleasedBeforeProbeCleanup"] = true
		var report map[string]any
		if err := wait(3*time.Second, func() bool {
			raw, err := os.ReadFile(filepath.Join(root, "data", "service-ownership.report.json"))
			return err == nil && json.Unmarshal(raw, &report) == nil
		}); err != nil {
			return result, errors.New("independent cleanup report is unavailable")
		}
		result["cleanupReport"] = report
		if report["observedCleanupConfirmed"] != true {
			return result, errors.New("monitor preserved unconfirmed ownership")
		}
		if mode == "concurrent-churn" {
			retired, retiredOK := report["retiredLifetimes"].(float64)
			retained, retainedOK := report["retainedLifetimes"].(float64)
			total, totalOK := report["observedLifetimes"].(float64)
			if !retiredOK || !retainedOK || !totalOK || retired == 0 || retained >= total {
				return result, errors.New("completed process history was not reclaimed")
			}
			result["completedHistoryReclaimed"] = true
		}
		for _, dir := range []string{"data", "installation"} {
			lease, err := lifecycle.Acquire(filepath.Join(root, dir))
			if err != nil {
				return result, err
			}
			lease.Close()
		}
		result["serviceLeasesReleased"] = true
	}
	if result["unrelatedPreserved"] != true {
		return result, errors.New("unrelated sentinel was stopped")
	}
	return result, nil
}

func run() error {
	if len(os.Args) == 2 && (os.Args[1] == "short-churn" || os.Args[1] == "concurrent-churn") {
		root, err := os.MkdirTemp(".state", "service-guard-risk-")
		if err != nil {
			return err
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		value, err := runCase(root, os.Args[1])
		report := map[string]any{"root": root, "case": value, "passed": err == nil, "modelRequests": 0, "packagingValidation": false}
		_ = save(filepath.Join(root, "report.json"), report)
		raw, _ := json.Marshal(report)
		fmt.Println(string(raw))
		return err
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "process-owner", "process-child":
			return serviceguard.Dispatch(os.Args[1:])
		case "service":
			if len(os.Args) == 3 {
				return service(os.Args[2])
			}
		case "fixture":
			if len(os.Args) == 4 {
				return fixture(os.Args[2], os.Args[3])
			}
		case "resume":
			if len(os.Args) == 3 {
				var expected processidentity.Identity
				if json.Unmarshal([]byte(os.Args[2]), &expected) != nil {
					return errors.New("invalid bounded resume identity")
				}
				time.Sleep(3 * time.Second)
				err := pauseSignal(expected, syscall.SIGCONT)
				if errors.Is(err, syscall.ESRCH) || errors.Is(err, processidentity.ErrChanged) {
					return nil
				}
				return err
			}
		}
		return errors.New("invalid bounded ownership fixture mode")
	}
	root, err := os.MkdirTemp(".state", "service-guard-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	results := []map[string]any{}
	var resultErr error
	for _, mode := range []string{"normal", "owner-killed", "watcher-killed", "fast-fork"} {
		value, err := runCase(root, mode)
		results = append(results, value)
		resultErr = errors.Join(resultErr, err)
	}
	report := map[string]any{"root": root, "cases": results, "packagingValidation": false, "officialModelRequests": 0, "passed": resultErr == nil}
	_ = save(filepath.Join(root, "report.json"), report)
	raw, _ := json.Marshal(report)
	fmt.Println(string(raw))
	return resultErr
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
