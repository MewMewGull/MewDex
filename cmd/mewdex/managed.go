package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mewdex/internal/localnode"
)

func managed(args []string) error {
	flags := flag.NewFlagSet("managed", flag.ContinueOnError)
	root := flags.String("root", "", "application-owned installation directory")
	owner := flags.String("owner", "", "startup registration owner")
	if err := flags.Parse(args); err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	manager, err := localnode.New(localnode.Options{Root: *root, Program: program})
	if err != nil {
		return err
	}
	log, err := localnode.OpenServiceLog(*root)
	if err != nil {
		return err
	}
	beforeOut, beforeErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = log, log
	defer func() { os.Stdout, os.Stderr = beforeOut, beforeErr; _ = log.Close() }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = manager.RunManaged(ctx, *owner, serve)
	if err != nil {
		fmt.Fprintln(log, "MewDex managed service:", err)
	}
	return err
}
