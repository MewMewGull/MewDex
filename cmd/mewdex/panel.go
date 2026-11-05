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

	"mewdex/internal/executorpanel"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
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
	return filepath.Join(filepath.Dir(program), "engine", "bin", name)
}

func panel(args []string) error {
	if distributionRole == "hub" {
		return errors.New("the executor package provides its control panel")
	}
	flags := flag.NewFlagSet("panel", flag.ContinueOnError)
	root := flags.String("root", ".local/executor", "executor configuration directory")
	listen := flags.String("listen", "127.0.0.1:47832", "local control panel listener")
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
	server := &http.Server{Addr: *listen, Handler: executorpanel.Handler(manager, strings.TrimSpace(string(raw))), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("MewDex executor panel:", "http://"+*listen)
	fmt.Println("Panel token file:", tokenPath)
	err = server.ListenAndServe()
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
