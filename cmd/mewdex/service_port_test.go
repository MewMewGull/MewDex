package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/serviceport"
)

func TestPortConflictPrecedesEngineAndDatabaseStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	data := t.TempDir()
	err = serveOwned(context.Background(), config.Config{DeviceID: "port-fixture", Hub: true, Executor: true, DataDir: data, Listen: listener.Addr().String(), Engine: config.Engine{Executable: filepath.Join(data, "missing-engine")}})
	var occupied *serviceport.OccupiedError
	if !errors.As(err, &occupied) {
		t.Fatal("startup did not report port conflict first", err)
	}
	for _, file := range []string{"hub.db", "executor.db", "control.json"} {
		if _, err := os.Stat(filepath.Join(data, file)); !os.IsNotExist(err) {
			t.Fatal("port conflict opened service state", file, err)
		}
	}
}
