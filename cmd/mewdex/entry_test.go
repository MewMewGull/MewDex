package main

import (
	"fmt"
	"os"
	"testing"

	"mewdex/internal/serviceguard"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "process-owner" || os.Args[1] == "process-child") {
		if err := serviceguard.Dispatch(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
