// Command perf measures the API against disposable PostgreSQL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mgrubiyan/pomnibot/backend/internal/perftest"
)

func main() {
	cfg := perftest.DefaultOptions()
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "check: enforce every stage; explore: find capacity while enforcing the 50-user study target and data integrity")
	flag.StringVar(&cfg.Only, "only", cfg.Only, "scenario group: all, study, concurrency, rate")
	flag.DurationVar(&cfg.Study, "study", cfg.Study, "study duration at each of 50 and 100 users")
	flag.DurationVar(&cfg.Stage, "stage", cfg.Stage, "duration of each concurrency and scheduled RPS stage")
	flag.DurationVar(&cfg.Think, "think", cfg.Think, "delay between answers")
	flag.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP request timeout")
	flag.DurationVar(&cfg.Warmup, "warmup", cfg.Warmup, "unmeasured warm-up per fresh database")
	flag.DurationVar(&cfg.Deadline, "deadline", cfg.Deadline, "maximum scheduler lateness before a missed request")
	flag.BoolVar(&cfg.Smoke, "smoke", false, "run only a short two-user functional check")
	out := flag.String("out", "", "optional JSON report path (no files written by default)")
	flag.Parse()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var snapshot func(perftest.Report) error
	if *out != "" {
		snapshot = func(r perftest.Report) error { return writeReport(*out, r) }
	}
	report, err := perftest.Run(ctx, cfg, os.Stdout, snapshot)
	if snapshot != nil {
		if writeErr := snapshot(report); writeErr != nil {
			fmt.Fprintln(os.Stderr, writeErr)
			os.Exit(1)
		}
		fmt.Println("Artifact:", *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeReport(path string, r perftest.Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
