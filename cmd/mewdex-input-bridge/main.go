package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"mewdex/internal/inputbridge"
)

func main() {
	root := flag.String("root", "", "APP channel data directory")
	flag.Parse()
	var options inputbridge.Options
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&options) != nil {
		fmt.Fprintln(os.Stderr, "Invalid APP channel connection")
		os.Exit(1)
	}
	options.Root = *root
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// The APP owns this process. Closing its pipe also closes platform connections.
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	if err := inputbridge.Run(ctx, options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
