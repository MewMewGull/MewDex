package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/localnode"
)

func managed(args []string) error {
	flags := flag.NewFlagSet("managed", flag.ContinueOnError)
	root := flags.String("root", "", "application-owned installation directory")
	owner := flags.String("owner", "", "startup registration owner")
	disableTray := flags.Bool("no-tray", false, "run without an interactive desktop tray")
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
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case received := <-signals:
			cancel(fmt.Errorf("managed service received %s", received))
		case <-ctx.Done():
		}
	}()
	fmt.Fprintf(log, "%s managed service starting pid=%d\n", time.Now().UTC().Format(time.RFC3339Nano), os.Getpid())
	err = manager.RunManaged(ctx, *owner, func(scope context.Context, c config.Config) error {
		c.DisableTray = *disableTray
		fmt.Fprintf(log, "%s managed service running pid=%d\n", time.Now().UTC().Format(time.RFC3339Nano), os.Getpid())
		return serve(scope, c)
	})
	fmt.Fprintf(log, "%s managed service exited pid=%d cause=%v result=%v\n", time.Now().UTC().Format(time.RFC3339Nano), os.Getpid(), context.Cause(ctx), err)
	if err != nil {
		fmt.Fprintln(log, "MewDex managed service:", err)
	}
	return err
}
