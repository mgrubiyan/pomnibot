//go:build perf

// Command perf measures the real API against a fresh, disposable PostgreSQL 17.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

type options struct {
	Only     string        `json:"only"`
	Study    time.Duration `json:"study"`
	Stage    time.Duration `json:"stage"`
	Think    time.Duration `json:"think"`
	Timeout  time.Duration `json:"timeout"`
	Warmup   time.Duration `json:"warmup"`
	Deadline time.Duration `json:"scheduling_deadline"`
	Smoke    bool          `json:"smoke"`
}

type report struct {
	Started    time.Time      `json:"started"`
	Revision   string         `json:"revision"`
	Go         string         `json:"go"`
	OS         string         `json:"os"`
	CPUs       int            `json:"cpus"`
	CPU        any            `json:"cpu"`
	Memory     any            `json:"memory"`
	Parameters options        `json:"parameters"`
	Fixtures   map[string]int `json:"fixtures"`
	Results    []stageResult  `json:"results"`
	Error      string         `json:"error,omitempty"`
}

func main() {
	cfg := options{}
	flag.StringVar(&cfg.Only, "only", "all", "scenario group: all, study, concurrency, rate")
	flag.DurationVar(&cfg.Study, "study", 5*time.Minute, "study duration at each of 50 and 100 users")
	flag.DurationVar(&cfg.Stage, "stage", 30*time.Second, "duration of each concurrency and scheduled RPS stage")
	flag.DurationVar(&cfg.Think, "think", 3*time.Second, "delay between answers")
	flag.DurationVar(&cfg.Timeout, "timeout", 5*time.Second, "HTTP request timeout")
	flag.DurationVar(&cfg.Warmup, "warmup", 3*time.Second, "unmeasured warm-up per fresh database")
	flag.DurationVar(&cfg.Deadline, "deadline", 100*time.Millisecond, "maximum scheduler lateness before a missed request")
	flag.BoolVar(&cfg.Smoke, "smoke", false, "run only a short two-user functional check")
	out := flag.String("out", "../docs/perf/results.json", "machine-readable artifact")
	flag.Parse()
	if cfg.Only != "all" && cfg.Only != "study" && cfg.Only != "concurrency" && cfg.Only != "rate" {
		fmt.Fprintln(os.Stderr, "invalid scenario group")
		os.Exit(2)
	}
	if cfg.Study <= 0 || cfg.Stage <= 0 || cfg.Think <= 0 || cfg.Timeout <= 0 || cfg.Deadline <= 0 || cfg.Warmup < 0 {
		fmt.Fprintln(os.Stderr, "durations must be positive (warmup may be zero)")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	info, _ := cpu.Info()
	memory, _ := mem.VirtualMemory()
	revision, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	r := report{Started: time.Now().UTC(), Revision: strings.TrimSpace(string(revision)), Go: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), CPU: info, Memory: memory, Parameters: cfg, Fixtures: map[string]int{"sets": 3, "facts": 150, "cards": 750, "background_users": 1000, "historical_answers": 100000, "existing_progress": 150150}}
	err := run(context.Background(), cfg, &r, *out)
	if err != nil {
		r.Error = err.Error()
	}
	data, marshalErr := json.MarshalIndent(r, "", "  ")
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, marshalErr)
		os.Exit(1)
	}
	if writeErr := os.WriteFile(*out, append(data, '\n'), 0600); writeErr != nil {
		fmt.Fprintln(os.Stderr, writeErr)
		os.Exit(1)
	}
	fmt.Println("Artifact:", *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg options, r *report, out string) error {
	type stage struct {
		name        string
		users, rate int
		duration    time.Duration
	}
	stages := []stage{{"study", 50, 0, cfg.Study}, {"study", 100, 0, cfg.Study}}
	for _, n := range []int{10, 25, 50, 100, 200, 400} {
		stages = append(stages, stage{"concurrency", n, 0, cfg.Stage})
	}
	// A scheduled request is one HTTP operation; the deterministic mix includes reads,
	// checks, batches of five writes, and teacher leaderboard requests.
	for _, rate := range []int{25, 50, 100, 200, 400, 800} {
		stages = append(stages, stage{"rate", 100, rate, cfg.Stage})
	}
	if cfg.Smoke {
		stages = []stage{{"study", 2, 0, cfg.Stage}}
	}
	failedGroup := ""
	targetFailed := false
	for _, s := range stages {
		if cfg.Only != "all" && cfg.Only != s.name {
			continue
		}
		if s.name == failedGroup {
			continue
		}
		fmt.Printf("Starting %s: users=%d rate=%d duration=%s\n", s.name, s.users, s.rate, s.duration)
		result, err := runStage(ctx, cfg, s.name, s.users, s.rate, s.duration)
		r.Results = append(r.Results, result)
		snapshot, snapshotErr := json.MarshalIndent(r, "", "  ")
		if snapshotErr != nil {
			return snapshotErr
		}
		if writeErr := os.WriteFile(out, append(snapshot, '\n'), 0600); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s users=%d rate=%d passed=%v startup=%v steady=%v RPS=%.1f p95=%.1fms p99=%.1fms errors=%d missed=%d journeys=%d\n", s.name, s.users, s.rate, result.Passed, result.StartupPassed, result.SteadyPassed, result.Steady.Overall.RPS, result.Steady.Overall.P95, result.Steady.Overall.P99, result.Steady.Overall.Errors, result.Missed, result.Journeys)
		if !result.Passed {
			failedGroup = s.name
			if s.name == "study" && s.users == 50 {
				targetFailed = true
			}
		}
		// Always measure 100-user headroom, even when the 50-user target fails.
		if s.name == "study" {
			failedGroup = ""
		}
	}
	if targetFailed {
		return fmt.Errorf("50-user demo criteria failed; see artifact")
	}
	return nil
}
