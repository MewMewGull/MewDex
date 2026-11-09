package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mewdex/internal/inputbridge"
	"mewdex/internal/messaging"
)

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader) error {
	flags := flag.NewFlagSet("mewdex-channel", flag.ContinueOnError)
	root := flags.String("root", "", "channel data directory")
	path := flags.String("config", "", "private standalone connection file")
	setup := flags.Bool("setup", false, "authorize a standalone connector using account JSON on standard input")
	base := flags.String("hub-url", "", "Hub address for setup")
	name := flags.String("name", "", "standalone connector display name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected connector arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *setup {
		return configure(ctx, *path, *base, *name, input)
	}
	var options inputbridge.Options
	if *path != "" {
		var err error
		options, err = readConnection(*path)
		if err != nil {
			return err
		}
		options.Standalone = true
		if *root == "" {
			*root = filepath.Join(filepath.Dir(*path), "channels")
		}
		if *name != "" {
			options.Name = *name
		}
		if options.Name == "" {
			host, _ := os.Hostname()
			options.Name = host + " · Message Connector"
		}
	} else {
		decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&options) != nil {
			return errors.New("invalid APP channel connection")
		}
		// Existing APP installations continue to own their pipe-based helper.
		go func() { _, _ = io.Copy(io.Discard, input); cancel() }()
	}
	options.Root = *root
	for {
		err := inputbridge.Run(ctx, options)
		if ctx.Err() != nil {
			return nil
		}
		if !options.Standalone || !errors.Is(err, messaging.ErrHubUnavailable) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}
