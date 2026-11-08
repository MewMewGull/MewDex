package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/localnode"
)

func upgrade(args []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	profile := flags.String("config", "", "existing device configuration")
	phase := flags.String("phase", "status", "prepare, apply or status")
	id := flags.String("id", "", "stable reviewed upgrade ID")
	device := flags.String("expected-device-id", "", "device identity shown by status")
	runtime := flags.String("engine-binary", packagedEngine(), "bundled runtime for this distribution")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *profile == "" || *id == "" || *device == "" || flags.NArg() != 0 {
		return errors.New("upgrade requires a configuration, stable ID and expected device identity")
	}
	if *phase != "prepare" && *phase != "apply" && *phase != "status" {
		return errors.New("upgrade phase must be prepare, apply or status")
	}
	path, err := filepath.Abs(*profile)
	if err != nil {
		return err
	}
	c, err := config.Read(path)
	if err != nil {
		return err
	}
	if err = validateDistribution(c); err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	result, err := localnode.UpgradeProfile(ctx, path, program, *runtime, localnode.Request{
		Operation: "upgrade." + *phase, ExpectedDeviceID: *device, Upgrade: &localnode.UpgradeRequest{ID: *id},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
