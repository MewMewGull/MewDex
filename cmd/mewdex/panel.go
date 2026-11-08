package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/executorpanel"
	"mewdex/internal/lifecycle"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
	"mewdex/internal/servicetray"
)

func packagedEngine() string {
	program, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "codex"
	if filepath.Ext(program) == ".exe" {
		name += ".exe"
	}
	if distributionRole == "executor" {
		sandbox := filepath.Join(filepath.Dir(program), "sandbox", "bin", "mewdex-sandbox"+filepath.Ext(program))
		if info, err := os.Stat(sandbox); err == nil && info.Mode().IsRegular() {
			return sandbox
		}
	}
	return filepath.Join(filepath.Dir(program), "engine", "bin", name)
}

func panel(args []string) error {
	flags := flag.NewFlagSet("panel", flag.ContinueOnError)
	root := flags.String("root", ".local/executor", "executor configuration directory")
	listen := flags.String("listen", "127.0.0.1:0", "local control panel listener")
	open := flags.Bool("open", false, "open the local panel in a browser")
	engine := flags.String("engine-binary", packagedEngine(), "executor sandbox binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return errors.New("serve the executor panel on a loopback address")
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	manager, err := localnode.New(localnode.Options{Root: *root, Program: program, Engine: *engine})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(*root, 0700); err != nil {
		return err
	}
	tokenPath := filepath.Join(*root, "panel-token")
	raw, err := os.ReadFile(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		raw = []byte(protocol.ID() + protocol.ID())
		file, createErr := os.OpenFile(tokenPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return createErr
		}
		_, err = file.Write(raw)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) < 32 {
		return errors.New("executor panel token is invalid")
	}
	key := strings.TrimSpace(string(raw))
	endpointPath := filepath.Join(*root, "panel.json")
	lease, err := lifecycle.Acquire(filepath.Join(*root, "panel"))
	if err != nil {
		var endpoint struct {
			URL string `json:"url"`
		}
		data, readErr := os.ReadFile(endpointPath)
		if readErr != nil || json.Unmarshal(data, &endpoint) != nil || !strings.HasPrefix(endpoint.URL, "http://127.0.0.1:") {
			return err
		}
		client := &http.Client{Timeout: 3 * time.Second}
		request, _ := http.NewRequest("POST", endpoint.URL+"/api/device", strings.NewReader(`{"operation":"status"}`))
		request.Header.Set("Authorization", "Bearer "+key)
		response, checkErr := client.Do(request)
		if checkErr != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			return err
		}
		if *open {
			return servicetray.OpenURL(endpoint.URL + "#" + key)
		}
		fmt.Println("MewDex panel", endpoint.URL)
		return nil
	}
	defer lease.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	address := "http://" + listener.Addr().String()
	if err = os.WriteFile(endpointPath, protocol.JSON(map[string]string{"url": address}), 0600); err != nil {
		return err
	}
	defer os.Remove(endpointPath)
	role := "executor"
	if distributionRole == "hub" {
		role = "hub"
	} else if distributionRole == "combined" {
		if configuration, readErr := config.Read(filepath.Join(*root, "device.json")); readErr == nil && configuration.Hub {
			role = "hub"
		}
	}
	server := &http.Server{Handler: executorpanel.HandlerForRole(manager, key, role), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("MewDex component panel:", address)
	fmt.Println("Panel token file:", tokenPath)
	if *open {
		go func() { _ = servicetray.OpenURL(address + "#" + key) }()
	}
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func initializeExecutor(path, hubURL, invitation string) error {
	if filepath.Base(path) != "device.json" {
		return errors.New("executor configuration must be named device.json")
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	manager, err := localnode.New(localnode.Options{Root: filepath.Dir(path), Program: program, Engine: packagedEngine()})
	if err != nil {
		return err
	}
	result, err := manager.Handle(context.Background(), localnode.Request{Operation: "setup", Setup: &localnode.Setup{Executor: true, HubURL: hubURL, Invitation: invitation, Projects: []protocol.Project{}}})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result.Snapshot)
}
