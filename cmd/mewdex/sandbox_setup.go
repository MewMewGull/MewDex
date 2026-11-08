package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/linuxuserns"
)

func sandboxSetup(args []string) error {
	flags := flag.NewFlagSet("sandbox-setup", flag.ContinueOnError)
	phase := flags.String("phase", "inspect", "inspect or apply the local Linux preparation")
	binary := flags.String("sandbox-binary", packagedEngine(), "packaged native sandbox binary")
	expected := flags.String("expected-sha256", "", "Bubblewrap digest from the inspected plan")
	confirmed := flags.Bool("confirm-userns", false, "explicitly authorize this helper's AppArmor user-namespace grant")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *binary == "" || flags.NArg() != 0 {
		return errors.New("select the packaged native sandbox with -sandbox-binary")
	}
	source := filepath.Join(filepath.Dir(*binary), "bwrap")
	var plan linuxuserns.Plan
	var err error
	switch *phase {
	case "inspect":
		plan, err = linuxuserns.Inspect(source)
	case "apply":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		plan, err = linuxuserns.Apply(ctx, source, *expected, *confirmed)
	default:
		return errors.New("select inspect or apply")
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(plan); encodeErr != nil {
		return errors.Join(err, encodeErr)
	}
	return err
}
