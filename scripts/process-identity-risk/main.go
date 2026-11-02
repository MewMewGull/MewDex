//go:build darwin

// Verify kernel birth ancestry and generation-checked signalling using only
// bounded owned fixtures. No model, installation, account or OS entry is used.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"mewdex/internal/lifecycle"
	"mewdex/internal/processidentity"
	"mewdex/internal/protocol"
)

func save(file string, value any) error { return os.WriteFile(file, protocol.JSON(value), 0600) }

func main() {
	var err error
	if len(os.Args) == 4 && os.Args[1] == "fixture" {
		err = fixture(os.Args[2], os.Args[3])
	} else {
		err = probe()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fixture(root, role string) error {
	if !filepath.IsAbs(root) || (role != "owner" && role != "detached" && role != "unrelated") {
		return errors.New("invalid bounded fixture")
	}
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err != nil {
		return err
	}
	defer lease.Close()
	if role == "owner" {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		child := exec.Command(self, "fixture", root, "detached")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err = child.Start(); err != nil {
			return err
		}
		go func() { _ = child.Wait() }()
	}
	if err = save(filepath.Join(root, role+".json"), map[string]int{"pid": os.Getpid()}); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(filepath.Join(root, role+"-stop")); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func held(root, role string) bool {
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err == nil {
		_ = lease.Close()
	}
	return err != nil
}

func ready(root, role string) (int, error) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(root, role+".json"))
		var value struct {
			PID int `json:"pid"`
		}
		if err == nil && json.Unmarshal(raw, &value) == nil && value.PID > 1 && held(root, role) {
			return value.PID, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("owned fixture %s is not ready", role)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func probe() (resultErr error) {
	root, err := os.MkdirTemp(".state", "process-identity-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "root": root, "state": "starting", "modelCalls": 0, "modelCredentialsRead": false, "sharedServicesChanged": false, "packagingValidation": false}
	if err = save(filepath.Join(root, "report.json"), report); err != nil {
		return err
	}
	fmt.Println("Process identity probe:", root)
	var owner, unrelated *exec.Cmd
	ownerWaited := false
	defer func() {
		var cleanupErr error
		for _, role := range []string{"owner", "detached", "unrelated"} {
			cleanupErr = errors.Join(cleanupErr, os.WriteFile(filepath.Join(root, role+"-stop"), nil, 0600))
		}
		if owner != nil && !ownerWaited {
			cleanupErr = errors.Join(cleanupErr, owner.Wait())
		}
		if unrelated != nil {
			cleanupErr = errors.Join(cleanupErr, unrelated.Wait())
		}
		deadline := time.Now().Add(2 * time.Second)
		for held(root, "detached") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if held(root, "owner") || held(root, "detached") || held(root, "unrelated") {
			cleanupErr = errors.Join(cleanupErr, errors.New("fixture cleanup is unconfirmed"))
		}
		report["cleanupConfirmed"] = cleanupErr == nil
		resultErr = errors.Join(resultErr, cleanupErr)
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"], report["error"] = "failed", resultErr.Error()
		}
		report["finishedAt"] = protocol.Now()
		_ = save(filepath.Join(root, "report.json"), report)
		fmt.Println(string(protocol.JSON(report)))
	}()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	owner = exec.Command(self, "fixture", root, "owner")
	if err = owner.Start(); err != nil {
		owner = nil
		return err
	}
	ownerPID, err := ready(root, "owner")
	if err != nil {
		return err
	}
	detachedPID, err := ready(root, "detached")
	if err != nil {
		return err
	}
	ownerIdentity, err := processidentity.Lookup(ownerPID)
	if err != nil {
		return err
	}
	detachedBefore, err := processidentity.Lookup(detachedPID)
	if err != nil {
		return err
	}
	if detachedBefore.ParentUniqueID != ownerIdentity.UniqueID || detachedBefore.Group != detachedPID {
		return errors.New("owned detached birth identity was not confirmed")
	}
	unrelated = exec.Command(self, "fixture", root, "unrelated")
	if err = unrelated.Start(); err != nil {
		unrelated = nil
		return err
	}
	unrelatedPID, err := ready(root, "unrelated")
	if err != nil {
		return err
	}
	unrelatedIdentity, err := processidentity.Lookup(unrelatedPID)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "owner-stop"), nil, 0600); err != nil {
		return err
	}
	if err = owner.Wait(); err != nil {
		ownerWaited = true
		return err
	}
	ownerWaited = true
	detachedAfter, err := processidentity.Lookup(detachedPID)
	if err != nil {
		return err
	}
	if detachedAfter.ParentPID == ownerPID || !detachedAfter.SameLifetime(detachedBefore) || detachedAfter.ParentUniqueID != ownerIdentity.UniqueID {
		return errors.New("reparented process lost its immutable birth edge")
	}
	listed, err := processidentity.ListPIDs()
	if err != nil {
		return err
	}
	detachedListed, unrelatedListed := false, false
	for _, pid := range listed {
		detachedListed = detachedListed || pid == detachedPID
		unrelatedListed = unrelatedListed || pid == unrelatedPID
	}
	if !detachedListed || !unrelatedListed {
		return errors.New("bounded native PID enumeration missed a live fixture")
	}
	found, err := processidentity.Discover([]processidentity.Identity{ownerIdentity}, []processidentity.Identity{unrelatedIdentity, detachedAfter})
	if err != nil || len(found) != 1 || !found[0].SameLifetime(detachedAfter) {
		return errors.New("ownership discovery adopted an unrelated process or missed its child")
	}
	wrong := detachedAfter
	wrong.UniqueID++
	if err = processidentity.Signal(wrong, syscall.SIGKILL); !errors.Is(err, processidentity.ErrChanged) || !held(root, "detached") {
		return errors.New("stale birth identity was not rejected")
	}
	wrong = detachedAfter
	wrong.PIDVersion++
	if err = processidentity.Signal(wrong, syscall.SIGKILL); !errors.Is(err, processidentity.ErrChanged) || !held(root, "detached") {
		return errors.New("stale exec generation was not rejected")
	}
	report["owner"], report["detachedBefore"], report["detachedAfter"] = ownerIdentity, detachedBefore, detachedAfter
	report["unrelated"], report["nativePIDEnumeration"], report["staleIdentitiesRejected"] = unrelatedIdentity, true, true
	report["state"] = "submitting verified owned signal"
	if err = save(filepath.Join(root, "report.json"), report); err != nil {
		return err
	}
	if err = processidentity.Signal(found[0], syscall.SIGKILL); err != nil {
		return fmt.Errorf("generation-checked owned signal: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for held(root, "detached") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if held(root, "detached") {
		return errors.New("generation-checked signal did not release its target lease")
	}
	report["ownedSignalConfirmed"], report["unrelatedPreserved"] = true, held(root, "unrelated")
	if report["unrelatedPreserved"] != true {
		return errors.New("owned signal affected the unrelated sentinel")
	}
	return nil
}
