// local-service-risk checks the packaged-helper boundary without starting a GUI
// and optionally checks the official engine in a fresh, unsigned home. It never
// creates a model turn, signs in, or starts a GUI.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/backup"
	"mewdex/internal/lifecycle"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("binary", ".state/dev/mewdex-desktop-helper.exe", "development service helper")
	enginePath := flag.String("engine-binary", "", "optional complete bundled engine entrypoint")
	verifyBackup := flag.Bool("backup", false, "verify encrypted backup through the real helper after stopping")
	verifyRestore := flag.Bool("restore", false, "verify prepared profile adoption, new pairing and restart in the isolated root")
	verifyJobs := flag.Bool("jobs", false, "run backup and restore through detached jobs and verify cancellation")
	reportOverride := flag.String("report", "", "optional new evidence path; never overwrite an existing file")
	flag.Parse()
	if *verifyRestore && !*verifyBackup {
		return errors.New("restore probe requires the encrypted backup probe")
	}
	if *verifyJobs && (!*verifyBackup || !*verifyRestore) {
		return errors.New("job probe requires backup and restore probes")
	}
	binary, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	if *enginePath != "" {
		for _, key := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "CODEX_API_KEY", "CHATGPT_BASE_URL"} {
			if os.Getenv(key) != "" {
				return errors.New("unsigned-home probe requires an environment without model credential or endpoint overrides")
			}
		}
		*enginePath, err = filepath.Abs(*enginePath)
		if err != nil {
			return err
		}
	}
	root, err := os.MkdirTemp(".state", "local-service-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	call := func(request localnode.Request) (localnode.Response, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "desktop", "-root", root)
		if *enginePath != "" {
			command.Args = append(command.Args, "-engine-binary", *enginePath)
		}
		command.Stdin = bytes.NewReader(mustJSON(request))
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &bytes.Buffer{}
		if err := command.Run(); err != nil {
			return localnode.Response{}, fmt.Errorf("helper process failed during %s", request.Operation)
		}
		var response localnode.Response
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			return response, errors.New("helper response was not structured JSON")
		}
		if response.Version != 1 {
			return response, errors.New("unexpected local helper protocol")
		}
		return response, nil
	}
	expect := func(request localnode.Request) (localnode.Result, error) {
		response, err := call(request)
		if err != nil {
			return localnode.Result{}, err
		}
		if !response.OK || response.Result == nil {
			return localnode.Result{}, fmt.Errorf("helper rejected %s: %s", request.Operation, response.Error)
		}
		return *response.Result, nil
	}
	waitJob := func(id string) (localnode.Result, error) {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			result, err := expect(localnode.Request{Operation: "job.status", Job: &localnode.JobRequest{ID: id}})
			if err != nil {
				return result, err
			}
			if result.Job == nil {
				return result, errors.New("detached operation lost its durable intent")
			}
			if result.Job.State != "starting" && result.Job.State != "running" {
				return result, nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return localnode.Result{}, errors.New("detached operation is still unconfirmed; its ID was not replayed")
	}
	perform := expect
	if *verifyJobs {
		perform = func(request localnode.Request) (localnode.Result, error) {
			job := &localnode.JobRequest{ID: protocol.ID(), Operation: request.Operation, ExpectedDeviceID: request.ExpectedDeviceID, Backup: request.Backup, Restore: request.Restore}
			if request.Operation == "restore.prepare" {
				job.Restore.ID = job.ID
			}
			accepted, err := expect(localnode.Request{Operation: "job.start", Job: job})
			if err != nil || accepted.Job == nil || accepted.Job.ID != job.ID {
				return accepted, errors.New("detached operation was not accepted")
			}
			// Each read has a new helper parent. The original parent has exited.
			observed, err := expect(localnode.Request{Operation: "job.status"})
			if err != nil {
				return observed, fmt.Errorf("a new input could not read its latest operation: %w", err)
			}
			if observed.Job == nil || observed.Job.ID != job.ID {
				return observed, errors.New("a new input could not find the latest operation")
			}
			completed, err := waitJob(job.ID)
			if err != nil {
				return completed, err
			}
			if completed.Job.State != "succeeded" {
				return completed, fmt.Errorf("detached %s did not succeed: %s", job.Operation, completed.Job.Error)
			}
			// An explicit duplicate probe must observe the retained result.
			again, err := expect(localnode.Request{Operation: "job.start", Job: job})
			if err != nil || again.Job == nil || again.Job.ID != job.ID || again.Job.State != "succeeded" {
				return again, errors.New("same operation ID did not retain its completed result")
			}
			completed.Backup, completed.Restore = completed.Job.Backup, completed.Job.Restore
			return completed, nil
		}
	}
	var started bool
	defer func() {
		if started {
			_, _ = call(localnode.Request{Operation: "stop"})
		}
	}()
	initial, err := expect(localnode.Request{Operation: "status"})
	if err != nil {
		return err
	}
	if initial.Snapshot.Configured {
		return errors.New("risk directory unexpectedly has a profile")
	}
	setup := &localnode.Setup{Hub: true, Listen: "127.0.0.1:0", Name: "MewDex isolated service probe"}
	if *enginePath != "" {
		setup.Executor = true
		projectPath := filepath.Join(root, "project")
		if err = os.Mkdir(projectPath, 0700); err != nil {
			return err
		}
		setup.Projects = []protocol.Project{{ID: "probe-project", Name: "Isolated probe", Path: projectPath}}
	}
	configured, err := expect(localnode.Request{Operation: "setup", Setup: setup})
	if err != nil {
		return err
	}
	if !configured.Snapshot.Configured || configured.Snapshot.Executor != setup.Executor || configured.Snapshot.Ready {
		return errors.New("setup reported readiness or incorrect executor capability")
	}
	started = true // A lost start response must still trigger cleanup.
	first, err := expect(localnode.Request{Operation: "start"})
	if err != nil {
		return fmt.Errorf("%w; diagnostics retained at %s", err, root)
	}
	started = true
	if !first.Snapshot.Ready || first.Snapshot.BaseURL == "" {
		return errors.New("service reported startup before its Hub was ready")
	}
	// Every call is a new helper process; previous helper parents have exited.
	second, err := expect(localnode.Request{Operation: "start"})
	if err != nil {
		return err
	}
	if first.Snapshot.InstanceID == "" || first.Snapshot.InstanceID != second.Snapshot.InstanceID {
		return errors.New("a repeated start created another service")
	}
	wrong, err := call(localnode.Request{Operation: "stop", ExpectedInstanceID: "a-different-instance"})
	if err != nil {
		return err
	}
	if wrong.OK {
		return errors.New("stale instance request stopped a service")
	}
	connected, err := expect(localnode.Request{Operation: "connect"})
	if err != nil {
		return err
	}
	if connected.Connection == nil || connected.Connection.Credentials.Token == "" {
		return errors.New("local administrator input pairing failed")
	}
	request, err := http.NewRequest("GET", connected.Connection.BaseURL+"/v1/bootstrap", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+connected.Connection.Credentials.Token)
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("paired input could not read its network snapshot")
	}
	if setup.Executor {
		if err = readUnsignedAccount(client, *connected.Connection, first.Snapshot.DeviceID); err != nil {
			return err
		}
	}
	stopped, err := expect(localnode.Request{Operation: "stop", ExpectedDeviceID: first.Snapshot.DeviceID, ExpectedInstanceID: first.Snapshot.InstanceID})
	if err != nil {
		return err
	}
	if stopped.Snapshot.Ready || stopped.Snapshot.State != "stopped" {
		return errors.New("stop was not confirmed")
	}
	started = false
	lock, err := lifecycle.Acquire(filepath.Join(root, "data"))
	if err != nil {
		return errors.New("service data remains owned after stop")
	}
	lock.Close()
	if *verifyBackup {
		archive := root + ".mewdex-backup"
		passphrase := protocol.ID() + protocol.ID()
		backed, err := perform(localnode.Request{Operation: "backup", ExpectedDeviceID: first.Snapshot.DeviceID, Backup: &localnode.BackupRequest{Path: archive, Passphrase: passphrase}})
		if err != nil {
			return err
		}
		if backed.Backup == nil || backed.Backup.Files == 0 || backed.Backup.DeviceID != first.Snapshot.DeviceID || backed.Snapshot.State != "stopped" {
			return errors.New("backup helper returned an invalid result")
		}
		if _, err = backup.Extract(context.Background(), archive, passphrase, root, "restored", backup.Limits{}); err != nil {
			return err
		}
		restored, err := store.Open(filepath.Join(root, "restored", "data", "hub.db"))
		if err != nil {
			return err
		}
		identity, err := restored.Authenticate(connected.Connection.Credentials.Token)
		restored.Close()
		if err != nil || identity.Role != "admin" {
			return errors.New("encrypted backup did not retain its original input identity")
		}
		if *verifyRestore {
			prepared, err := perform(localnode.Request{Operation: "restore.prepare", Restore: &localnode.RestoreRequest{Path: archive, Passphrase: passphrase}})
			if err != nil {
				return err
			}
			if prepared.Restore == nil || prepared.Restore.Applied || !prepared.Restore.RequiresPairing {
				return errors.New("restore preparation did not return its access consequences")
			}
			id := prepared.Restore.ID
			applied, err := perform(localnode.Request{Operation: "restore.apply", Restore: &localnode.RestoreRequest{ID: id}})
			if err != nil {
				return err
			}
			if applied.Restore == nil || !applied.Restore.Applied || applied.Snapshot.State != "stopped" {
				return errors.New("restore profile was not adopted while stopped")
			}
			observed, err := expect(localnode.Request{Operation: "restore.status", Restore: &localnode.RestoreRequest{ID: id}})
			if err != nil || observed.Restore == nil || !observed.Restore.Applied {
				return errors.New("lost restore receipt could not be observed")
			}
			started = true
			restarted, err := expect(localnode.Request{Operation: "start"})
			if err != nil {
				return err
			}
			if !restarted.Snapshot.Ready || restarted.Snapshot.InstanceID == first.Snapshot.InstanceID {
				return errors.New("restored service did not start a new instance")
			}
			request, err := http.NewRequest("GET", restarted.Snapshot.BaseURL+"/v1/me", nil)
			if err != nil {
				return err
			}
			request.Header.Set("Authorization", "Bearer "+connected.Connection.Credentials.Token)
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				return errors.New("restored Hub reactivated an old input credential")
			}
			fresh, err := expect(localnode.Request{Operation: "connect"})
			if err != nil {
				return err
			}
			if fresh.Connection == nil || fresh.Connection.Credentials.Token == connected.Connection.Credentials.Token {
				return errors.New("restored Hub did not issue a new input identity")
			}
			if setup.Executor {
				if err = readUnsignedAccount(client, *fresh.Connection, first.Snapshot.DeviceID); err != nil {
					return err
				}
			}
			blocked, err := call(localnode.Request{Operation: "projects", Projects: setup.Projects})
			if err != nil || blocked.OK {
				return errors.New("running installation did not protect its profile")
			}
			stopped, err = expect(localnode.Request{Operation: "stop", ExpectedDeviceID: restarted.Snapshot.DeviceID, ExpectedInstanceID: restarted.Snapshot.InstanceID})
			if err != nil || stopped.Snapshot.State != "stopped" {
				return errors.New("restored service stop was not confirmed")
			}
			started = false
			lease, err := lifecycle.Installation(root)
			if err != nil {
				return errors.New("stopped service retained its installation lease")
			}
			lease.Close()
		}
	}
	if *verifyJobs {
		cancelID, destination := protocol.ID(), root+".cancelled-backup"
		request := &localnode.JobRequest{ID: cancelID, Operation: "backup", Backup: &localnode.BackupRequest{Path: destination, Passphrase: protocol.ID() + protocol.ID()}}
		if _, err = expect(localnode.Request{Operation: "job.start", Job: request}); err != nil {
			return err
		}
		if _, err = expect(localnode.Request{Operation: "job.cancel", Job: &localnode.JobRequest{ID: cancelID}}); err != nil {
			return err
		}
		cancelled, err := waitJob(cancelID)
		if err != nil || cancelled.Job == nil || cancelled.Job.State != "cancelled" {
			return errors.New("detached backup cancellation was not confirmed")
		}
		if _, err = os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
			return errors.New("cancelled detached backup retained a partial output")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "engine-home")); !setup.Executor && !errors.Is(err, os.ErrNotExist) {
		return errors.New("Hub-only probe unexpectedly created an engine home")
	}
	report := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "root": root, "parentHelpersExited": true, "repeatedStartSameInstance": true, "staleStopRejected": true, "inputPaired": true, "serviceStopped": true, "hubOnly": !setup.Executor, "engineStarted": setup.Executor, "unsignedAccountRead": setup.Executor, "inferenceRun": false, "uiLaunched": false, "finalCandidate": false}
	report["encryptedBackupVerified"] = *verifyBackup
	report["profileRestoreApplied"] = *verifyRestore
	report["oldInputRejectedAfterRestore"] = *verifyRestore
	report["newInputPairedAfterRestore"] = *verifyRestore
	report["restoredServiceRestartVerified"] = *verifyRestore
	report["backgroundJobsVerified"] = *verifyJobs
	report["cancelledBackupVerified"] = *verifyJobs
	reportPath := ".state/evidence/local-service-risk.json"
	if setup.Executor {
		reportPath = ".state/evidence/local-service-engine-risk.json"
	}
	if *verifyBackup {
		reportPath = strings.TrimSuffix(reportPath, ".json") + "-backup.json"
	}
	if *verifyRestore {
		reportPath = strings.TrimSuffix(reportPath, "-backup.json") + "-restore.json"
	}
	if *verifyJobs {
		reportPath = strings.TrimSuffix(reportPath, ".json") + "-jobs.json"
	}
	if *reportOverride != "" {
		file, err := os.OpenFile(*reportOverride, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(mustJSON(report))
		closeErr := file.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(reportPath, mustJSON(report), 0600); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func readUnsignedAccount(client http.Client, connection localnode.Connection, deviceID string) error {
	command := protocol.Command{ID: protocol.ID(), DeviceID: deviceID, Kind: "account.read"}
	request, err := http.NewRequest("POST", connection.BaseURL+"/v1/commands", bytes.NewReader(mustJSON(command)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+connection.Credentials.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return errors.New("engine account read was not accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		request, err = http.NewRequestWithContext(ctx, "GET", connection.BaseURL+"/v1/commands/"+command.ID, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+connection.Credentials.Token)
		response, err = client.Do(request)
		if err != nil {
			return errors.New("engine account-read receipt was not confirmed")
		}
		var result protocol.CommandResult
		err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			return errors.New("invalid engine account-read receipt")
		}
		if result.State == "completed" {
			var account protocol.ModelAccount
			if json.Unmarshal(result.Data, &account) != nil || account.SignedIn || !account.LoginSupported {
				return errors.New("isolated engine did not report an unsigned, dedicated home")
			}
			return nil
		}
		if result.State != "accepted" {
			return errors.New("engine account read failed or became unconfirmed")
		}
		select {
		case <-ctx.Done():
			return errors.New("engine account read timed out")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
