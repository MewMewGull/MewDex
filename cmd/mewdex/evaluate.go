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
	"syscall"
	"time"

	"mewdex/internal/evaluation"
)

func evaluate(args []string) error {
	if distributionRole == "executor" {
		return errors.New("run evaluations with the Hub package or the development CLI")
	}
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	suitePath := flags.String("suite", "", "versioned evaluation suite JSON")
	connectionPath := flags.String("connection", "-", "input connection JSON path, or - for standard input")
	output := flags.String("output", "", "dedicated resumable evaluation directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum observation time per case")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *suitePath == "" || *output == "" || flags.NArg() != 0 {
		return errors.New("provide -suite and a dedicated -output directory")
	}
	file, err := os.Open(*suitePath)
	if err != nil {
		return err
	}
	defer file.Close()
	var suite evaluation.Suite
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&suite); err != nil {
		return fmt.Errorf("invalid evaluation suite %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("provide one bounded evaluation suite")
	}
	var input io.Reader = os.Stdin
	if *connectionPath != "-" {
		connection, err := os.Open(*connectionPath)
		if err != nil {
			return err
		}
		defer connection.Close()
		input = connection
	}
	var connection evaluation.Connection
	decoder = json.NewDecoder(io.LimitReader(input, 1<<20))
	if decoder.Decode(&connection) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("provide one input connection as JSON without putting its token in command arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	report, err := evaluation.Run(ctx, connection, suite, *output, *timeout)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(map[string]any{"runId": report.RunID, "complete": report.Complete, "passed": report.Passed, "cases": len(report.Cases), "output": *output}); encodeErr != nil {
		return errors.Join(err, encodeErr)
	}
	if err != nil {
		return err
	}
	if !report.Passed {
		return errors.New("evaluation checks failed, inspect review.md and the saved traces")
	}
	return nil
}
