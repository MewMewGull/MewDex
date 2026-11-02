// mewdex-backhaul is an optional self-hosting operations helper. It keeps the
// existing operator-provisioned SSH ingress alive without holding a Codex turn.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mewdex/internal/backhaul"
	"mewdex/internal/lifecycle"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/serviceguard"
	"mewdex/internal/startup"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "MewDex backhaul:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mewdex-backhaul serve|managed|status|stop|enable|disable|start -root path")
	}
	mode := args[0]
	if mode == "process-owner" || mode == "process-child" {
		return serviceguard.Dispatch(args)
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	rootArg := flags.String("root", "", "owned backhaul installation directory")
	owner := flags.String("owner", "", "startup registration owner")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *rootArg == "" || len(flags.Args()) != 0 {
		return errors.New("an explicit backhaul root is required")
	}
	root, err := filepath.Abs(*rootArg)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if mode == "status" {
		value, err := backhaul.Status(ctx, root)
		if err != nil {
			return err
		}
		if backend, err := startup.Native(); err == nil {
			if controller, err := startup.New(root, backend); err == nil {
				_, binding, _ := backhaul.Read(root)
				if info, err := controller.Status(ctx, binding); err == nil {
					value["startup"] = info
				} else {
					value["startupError"] = err.Error()
				}
			}
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	if mode == "stop" {
		record, err := lifecycle.Read(filepath.Join(root, "data"))
		if err != nil {
			return err
		}
		stop, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		value, err := lifecycle.Call(stop, record, true)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	backend, err := startup.Native()
	if err != nil {
		return err
	}
	controller, err := startup.New(root, backend)
	if err != nil {
		return err
	}
	if mode == "disable" {
		value, err := controller.Apply(ctx, false, "", startup.Binding{})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	}
	c, binding, err := backhaul.Read(root)
	if err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	switch mode {
	case "enable":
		if err := c.VerifySSH(); err != nil {
			return err
		}
		staged, err := runtimefiles.Program(root, program)
		if err != nil {
			return err
		}
		value, err := controller.Apply(ctx, true, staged, binding)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(value)
	case "start":
		return controller.Start(ctx, binding)
	case "managed":
		err := backhaul.RunManaged(ctx, root, c, binding, controller, *owner, program)
		if errors.Is(err, backhaul.ErrProfileChanged) {
			return nil // Never adopt a changed configuration on a retry or login.
		}
		return err
	case "serve":
		return backhaul.Run(ctx, root, c, binding)
	default:
		return errors.New("unknown backhaul operation")
	}
}
