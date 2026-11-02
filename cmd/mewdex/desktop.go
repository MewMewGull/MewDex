package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"mewdex/internal/localnode"
)

func desktopManager(args []string) (*localnode.Manager, error) {
	flags := flag.NewFlagSet("desktop", flag.ContinueOnError)
	root := flags.String("root", "", "app-owned local device root")
	engine := flags.String("engine-binary", "", "bundled native engine path")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	program, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return localnode.New(localnode.Options{Root: *root, Program: program, Engine: *engine})
}

func desktop(args []string) error {
	manager, err := desktopManager(args)
	if err != nil {
		return err
	}
	var request localnode.Request
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return errors.New("invalid local service request")
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("one local service request is required")
	}
	timeout := 30 * time.Second
	if request.Operation == "service.update" {
		timeout = 90 * time.Second
	}
	if request.Operation == "backup" || request.Operation == "restore.prepare" || request.Operation == "restore.apply" {
		timeout = 9 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := manager.Handle(ctx, request)
	response := localnode.Response{Version: 1, OK: err == nil}
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Result = &result
	}
	return json.NewEncoder(os.Stdout).Encode(response)
}

func desktopJob(args []string) error {
	manager, err := desktopManager(args)
	if err != nil {
		return err
	}
	var request localnode.JobRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid local operation delivery")
	}
	return manager.RunJob(context.Background(), request)
}
