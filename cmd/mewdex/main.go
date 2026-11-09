package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/computeruse"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/hostidentity"
	"mewdex/internal/hub"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/serviceguard"
	"mewdex/internal/serviceport"
	"mewdex/internal/servicetray"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

func main() {
	if err := run(); err != nil {
		if len(os.Args) == 1 {
			servicetray.ShowError("MewDex", err.Error())
		}
		fmt.Fprintln(os.Stderr, "MewDex:", err)
		os.Exit(1)
	}
}

// Set by the component packager. Existing combined installations keep their CLI.
var distributionRole = "combined"

func run() error {
	if len(os.Args) < 2 {
		if distributionRole != "combined" && (runtime.GOOS == "windows" || runtime.GOOS == "darwin") {
			directory, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			return panel([]string{"-root", filepath.Join(directory, "MewDex", distributionRole), "-open"})
		}
		return errors.New("usage: mewdex init|account|serve|stop|status|invite|doctor -config path, mewdex evaluate -suite file -output directory, or mewdex sandbox-setup -phase inspect")
	}
	mode := os.Args[1]
	if mode == "tray-helper" {
		return servicetray.RunHelper()
	}
	if mode == "open-url-helper" {
		return servicetray.RunOpenHelper()
	}
	if mode == "sandbox-setup" {
		return sandboxSetup(os.Args[2:])
	}
	if mode == "evaluate" {
		return evaluate(os.Args[2:])
	}
	if mode == "computer-native" {
		return computeruse.ServeNative(os.Stdin, os.Stdout)
	}
	if mode == "panel" {
		return panel(os.Args[2:])
	}
	if mode == "process-owner" || mode == "process-child" {
		return serviceguard.Dispatch(os.Args[1:])
	}
	if mode == "managed" {
		return managed(os.Args[2:])
	}
	if mode == "desktop" {
		return desktop(os.Args[2:])
	}
	if mode == "desktop-job" {
		return desktopJob(os.Args[2:])
	}
	if mode == "install" {
		return install(os.Args[2:])
	}
	if mode == "upgrade" {
		return upgrade(os.Args[2:])
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	defaultPath := ".local/device.json"
	if distributionRole != "combined" {
		defaultPath = ".local/" + distributionRole + "/device.json"
	}
	path := flags.String("config", defaultPath, "device config path")
	hubURL := flags.String("hub-url", "", "Hub endpoint for executor setup")
	invitation := flags.String("invitation", "", "executor invitation for setup")
	role := flags.String("role", "client", "pairing identity role")
	probe := flags.Bool("engine", false, "check the Hub model engine or executor sandbox without inference")
	probeGateway := flags.Bool("gateway", false, "probe the official service through the configured proxy without model credentials")
	installRoot := flags.String("install-root", "", "trusted native installation root for the service lifetime lease")
	disableTray := flags.Bool("no-tray", false, "run without an interactive desktop tray")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if mode == "init" {
		if distributionRole == "executor" {
			return initializeExecutor(*path, *hubURL, *invitation)
		}
		return initialize(*path)
	}
	var c config.Config
	var err error
	if mode == "stop" || mode == "status" || mode == "account" {
		c, err = config.Read(*path)
	} else {
		c, err = config.Load(*path)
	}
	if err != nil {
		return err
	}
	if err = validateDistribution(c); err != nil {
		return err
	}
	c.DisableTray = *disableTray
	if mode == "account" {
		if !c.Hub {
			return errors.New("MewDex accounts belong to the central Hub")
		}
		var login protocol.MewDexAccountLogin
		decoder := json.NewDecoder(io.LimitReader(os.Stdin, 8192))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&login); err != nil {
			return errors.New("provide MewDex username and password as JSON on standard input")
		}
		s, openErr := store.Open(filepath.Join(c.DataDir, "hub.db"))
		if openErr != nil {
			return openErr
		}
		defer s.Close()
		if err = s.ProvisionMewDexAccount(login); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"accountConfigured": true, "modelAccountChanged": false})
	}
	if mode == "stop" || mode == "status" {
		record, err := lifecycle.Read(c.DataDir)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := lifecycle.Call(ctx, record, mode == "stop")
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if mode == "doctor" {
		exe, args, resolveErr := engine.Resolve(c.Engine)
		report := map[string]any{"platform": runtime.GOOS + "/" + runtime.GOARCH, "deviceId": c.DeviceID, "hub": c.Hub, "executor": c.Executor, "projects": len(c.Projects), "engineExecutable": exe, "engineArgs": args, "engineResolved": resolveErr == nil, "inferenceRun": false}
		if *probe && c.Executor && !c.Hub {
			settings := c.Engine
			settings.Home = filepath.Join(c.DataDir, "terminal-runtime")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			sandbox, sandboxErr := engine.NewSandbox(ctx, settings)
			cancel()
			report["localSandboxAvailable"] = sandboxErr == nil
			if sandboxErr != nil {
				report["localSandboxError"] = sandboxErr.Error()
			} else {
				report["localSandboxVersion"] = sandbox.Version()
			}
		}
		if *probeGateway && c.Hub {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			result, err := engine.ProbeProxy(ctx, c.HubModelEngine())
			cancel()
			if err != nil {
				report["gatewayError"] = err.Error()
			} else {
				report["gateway"] = result
			}
		}
		if *probe && c.Hub {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			e, err := engine.Start(ctx, c.HubModelEngine())
			if err != nil {
				report["handshakeError"] = err.Error()
			} else {
				report["handshake"] = true
				var account struct {
					Account *struct {
						Type string `json:"type"`
					} `json:"account"`
				}
				if err = e.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err == nil {
					if account.Account == nil {
						report["modelAuth"] = "not-signed-in"
					} else {
						report["modelAuth"] = account.Account.Type
					}
				}
				_ = e.Close()
			}
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	if mode == "invite" {
		s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
		if err != nil {
			return err
		}
		defer s.Close()
		code, err := s.Invite(*role, 10*time.Minute)
		if err != nil {
			return err
		}
		fmt.Println(code)
		return nil
	}
	if mode != "serve" {
		return errors.New("unknown subcommand")
	}
	if *installRoot != "" {
		absolute, err := filepath.Abs(*installRoot)
		if err != nil {
			return err
		}
		profilePath, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		if absolute != filepath.Dir(profilePath) {
			return errors.New("installation root must contain its service profile")
		}
		c.InstallationRoot = absolute
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serve(ctx, c)
}

func initialize(path string) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("config already exists; no changes made")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	name, _ := os.Hostname()
	c := config.Config{DeviceID: protocol.ID(), Name: name, DataDir: "data", Listen: "127.0.0.1:47830", Hub: true, Executor: true, Projects: []protocol.Project{{ID: "mewdex", Name: filepath.Base(cwd), Path: cwd}}, AllowedOrigins: []string{"http://localhost:1420", "http://127.0.0.1:1420", "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost"}}
	if distributionRole == "hub" {
		c.Executor, c.Projects = false, []protocol.Project{}
		c.Engine = config.Engine{Executable: packagedEngine(), Home: "engine-home"}
		c.AllowedOrigins = append(c.AllowedOrigins, "http://127.0.0.1:47831", "http://localhost:47831")
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(b, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	s, err := store.Open(filepath.Join(filepath.Dir(abs), "data", "hub.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	code, err := s.Invite("admin", 10*time.Minute)
	if err != nil {
		return err
	}
	codePath := filepath.Join(filepath.Dir(abs), "pairing-code")
	if err = os.WriteFile(codePath, []byte(code), 0600); err != nil {
		return err
	}
	fmt.Println("Configuration:", abs)
	fmt.Println("One-time admin pairing code:", codePath)
	return nil
}

func serve(ctx context.Context, c config.Config) error {
	if err := validateDistribution(c); err != nil {
		return err
	}
	var installation *lifecycle.Lock
	if c.InstallationRoot != "" {
		var err error
		installation, err = lifecycle.Installation(c.InstallationRoot)
		if err != nil {
			return err
		}
		defer installation.Close()
	}
	return serviceguard.Run(ctx, c.DataDir, installation, c.Executor || c.Hub, func(ctx context.Context) error { return serveOwned(ctx, c) })
}

func validateDistribution(c config.Config) error {
	if distributionRole == "hub" && (!c.Hub || c.Executor) {
		return errors.New("the Hub package requires a Hub-only profile")
	}
	if distributionRole == "executor" && (c.Hub || !c.Executor) {
		return errors.New("the executor package requires an executor-only profile")
	}
	return nil
}

func serveOwned(ctx context.Context, c config.Config) error {
	hostID, err := hostidentity.ID(ctx)
	if err != nil {
		return fmt.Errorf("verify service host identity: %w", err)
	}
	c.HostID = hostID
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	// Reserve the configured port before opening databases or starting an engine.
	var hubListener net.Listener
	if c.Hub {
		var err error
		hubListener, err = serviceport.Listen(c.Listen)
		if err != nil {
			return err
		}
		defer hubListener.Close()
	}
	errorsCh := make(chan error, 2)
	control, err := lifecycle.Start(c.DataDir, c.DeviceID, func() {
		fmt.Fprintf(os.Stderr, "%s local service stop requested pid=%d\n", time.Now().UTC().Format(time.RFC3339Nano), os.Getpid())
		stop()
	})
	if err != nil {
		return err
	}
	defer control.Close()
	var h *hub.Hub
	var hs *store.Store
	if c.Hub {
		var err error
		hs, err = store.Open(filepath.Join(c.DataDir, "hub.db"))
		if err != nil {
			return err
		}
		defer hs.Close()
		if _, err = hs.RecoverCommands(""); err != nil {
			return err
		}
		h = hub.New(c, hs)
		h.SetContext(ctx)
		assets, err := attachments.New(filepath.Join(c.DataDir, "attachments"))
		if err != nil {
			return err
		}
		h.SetAttachments(assets)
	}
	roles := []string{}
	if c.Hub {
		roles = append(roles, "hub")
	}
	if c.Executor {
		roles = append(roles, "executor")
	}
	d := protocol.Device{ID: c.DeviceID, HostID: c.HostID, Name: c.Name, Platform: runtime.GOOS + "/" + runtime.GOARCH, Roles: roles, Projects: c.Projects, Online: true, ProtocolVersion: protocol.Version}
	if c.Executor {
		d.Capabilities = []string{"workspace-management", "hub-execution"}
		if available, _ := computeruse.NewNativeDriver("").Available(); available {
			d.Capabilities = append(d.Capabilities, "computer-use-v1")
		}
	}
	if c.Executor {
		s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
		if err != nil {
			return err
		}
		defer s.Close()
		var emit func(protocol.Event)
		if h != nil {
			emit = func(ev protocol.Event) {
				if _, err := h.Ingest(ev); err == nil {
					_ = s.Ack(ev.ID)
				}
			}
		}
		x, err := executor.NewTerminal(ctx, c, s)
		if err != nil {
			return err
		}
		defer x.Close()
		d.Projects = x.Projects()
		if h != nil {
			h.SetLocal(x)
			x.SetFilePublisher(h.PublishFileExport)
			if err = h.Device(d); err != nil {
				return err
			}
			for {
				pending, err := s.Unacked()
				if err != nil {
					return err
				}
				if len(pending) == 0 {
					break
				}
				for _, event := range pending {
					if _, err = h.Ingest(event); err != nil {
						return err
					}
					if err = s.Ack(event.ID); err != nil {
						return err
					}
				}
			}
			x.SetNotifier(emit)
		} else {
			x.SetFilePublisher(transport.FilePublisher(c))
			n := &transport.Node{Config: c, Store: s, Device: d, Executor: x}
			go func() {
				if err := n.Run(ctx); err != nil && ctx.Err() == nil {
					fmt.Fprintln(os.Stderr, "node connection:", err)
				}
			}()
		}
	}
	if h != nil {
		coordinator, err := h.StartCoordinator(ctx)
		if err != nil {
			return err
		}
		defer coordinator.Close()
		channels, err := store.Open(filepath.Join(c.DataDir, "message-connector", "channels.db"))
		if err != nil {
			return err
		}
		defer channels.Close()
		connector, err := h.StartMessaging(ctx, channels)
		if err != nil {
			return err
		}
		defer connector.Close()
		go func() {
			select {
			case <-ctx.Done():
			case <-coordinator.Done():
				if ctx.Err() == nil {
					errorsCh <- errors.New("Hub model engine exited unexpectedly; task outcomes require confirmation")
				}
			}
		}()
	}
	localHubURL := ""
	if h != nil {
		listener := hubListener
		host, port, _ := net.SplitHostPort(listener.Addr().String())
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			host = "127.0.0.1"
		}
		localHubURL = "http://" + net.JoinHostPort(host, port)
		h.SetBrowserOrigins(func() []string {
			origins := []string{localHubURL}
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				origins = append(origins, "http://"+net.JoinHostPort("localhost", port))
			}
			return origins
		})
		server := &http.Server{Addr: c.Listen, Handler: h.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			err := server.Serve(listener)
			if !errors.Is(err, http.ErrServerClosed) {
				errorsCh <- err
			}
		}()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		fmt.Println("MewDex Hub listening:", listener.Addr().String())
	}
	if ctx.Err() != nil {
		return nil
	}
	control.Ready(localHubURL)
	if (c.Hub || c.Executor) && c.ProfilePath != "" && !c.DisableTray {
		options, err := servicetray.Node(filepath.Dir(c.ProfilePath), stop)
		if err != nil {
			return err
		}
		closeTray, err := servicetray.Start(ctx, options)
		if err != nil {
			return err
		}
		defer closeTray()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errorsCh:
		return err
	}
}
