// Package perftest runs load scenarios against disposable PostgreSQL.
package perftest

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// Options configures a load run.
type Options struct {
	Mode     string        `json:"mode"`
	Only     string        `json:"only"`
	Study    time.Duration `json:"study"`
	Stage    time.Duration `json:"stage"`
	Think    time.Duration `json:"think"`
	Timeout  time.Duration `json:"timeout"`
	Warmup   time.Duration `json:"warmup"`
	Deadline time.Duration `json:"scheduling_deadline"`
	Smoke    bool          `json:"smoke"`
}

// Report records the environment and results of a load run.
type Report struct {
	Started    time.Time      `json:"started"`
	Revision   string         `json:"revision"`
	Go         string         `json:"go"`
	OS         string         `json:"os"`
	CPUs       int            `json:"cpus"`
	CPU        any            `json:"cpu"`
	Memory     any            `json:"memory"`
	Parameters Options        `json:"parameters"`
	Fixtures   map[string]int `json:"fixtures"`
	Results    []StageResult  `json:"results"`
	Error      string         `json:"error,omitempty"`
}

// DefaultOptions returns the standard load profile. Runs check every selected stage.
func DefaultOptions() Options {
	return Options{Mode: "check", Only: "all", Study: 5 * time.Minute, Stage: 30 * time.Second, Think: 3 * time.Second, Timeout: 5 * time.Second, Warmup: 3 * time.Second, Deadline: 100 * time.Millisecond}
}

// Validate rejects ambiguous scenarios and invalid timings before starting Docker.
func (cfg Options) Validate() error {
	if cfg.Mode != "check" && cfg.Mode != "explore" {
		return fmt.Errorf("mode must be check or explore")
	}
	if cfg.Only != "all" && cfg.Only != "study" && cfg.Only != "concurrency" && cfg.Only != "rate" {
		return fmt.Errorf("invalid scenario group")
	}
	if cfg.Smoke && cfg.Only != "all" {
		return fmt.Errorf("smoke cannot be combined with only")
	}
	if cfg.Study <= 0 || cfg.Stage <= 0 || cfg.Think <= 0 || cfg.Timeout <= 0 || cfg.Deadline <= 0 || cfg.Warmup < 0 {
		return fmt.Errorf("durations must be positive (warmup may be zero)")
	}
	if (cfg.Smoke || cfg.Only == "all" || cfg.Only == "concurrency") && cfg.Stage <= cfg.Think {
		return fmt.Errorf("stage must exceed think time to exercise answers")
	}
	if !cfg.Smoke && (cfg.Only == "all" || cfg.Only == "study") && cfg.Study <= cfg.Think {
		return fmt.Errorf("study must exceed think time to exercise answers")
	}
	return nil
}

// Run starts disposable PostgreSQL instances and returns a report even on failure.
// Snapshot is optional and receives intermediate reports after every stage.
func Run(ctx context.Context, cfg Options, output io.Writer, snapshot func(Report) error) (Report, error) {
	r := Report{Started: time.Now().UTC(), Go: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), Parameters: cfg, Fixtures: map[string]int{"sets": 3, "facts": 150, "cards": 750, "background_users": 1000, "historical_answers": 100000, "existing_progress": 150150}}
	if err := cfg.Validate(); err != nil {
		r.Error = err.Error()
		return r, err
	}
	info, _ := cpu.Info()
	memory, _ := mem.VirtualMemory()
	revision, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	r.CPU = info
	r.Memory = memory
	r.Revision = strings.TrimSpace(string(revision))
	err := runStages(ctx, cfg, &r, output, snapshot, runStage)
	if err != nil {
		r.Error = err.Error()
	}
	return r, err
}

type stageRunner func(context.Context, Options, string, int, int, time.Duration) (StageResult, error)

func runStages(ctx context.Context, cfg Options, r *Report, output io.Writer, snapshot func(Report) error, execute stageRunner) error {
	type stage struct {
		name        string
		users, rate int
		duration    time.Duration
	}
	stages := []stage{{"study", 50, 0, cfg.Study}, {"study", 100, 0, cfg.Study}}
	for _, n := range []int{10, 25, 50, 100, 200, 400} {
		stages = append(stages, stage{"concurrency", n, 0, cfg.Stage})
	}
	// Scheduled actions mix reads, answer journeys and teacher leaderboard reads.
	// An answer journey issues multiple HTTP requests; HTTP RPS is reported separately.
	for _, rate := range []int{25, 50, 100, 200, 400, 800} {
		stages = append(stages, stage{"rate", 100, rate, cfg.Stage})
	}
	if cfg.Smoke {
		stages = []stage{{"study", 2, 0, cfg.Stage}}
	}
	failedGroup := ""
	criteriaFailed := false
	for _, s := range stages {
		if cfg.Only != "all" && cfg.Only != s.name {
			continue
		}
		if s.name == failedGroup {
			continue
		}
		if _, err := fmt.Fprintf(output, "Starting %s: users=%d rate=%d duration=%s\n", s.name, s.users, s.rate, s.duration); err != nil {
			return err
		}
		result, err := execute(ctx, cfg, s.name, s.users, s.rate, s.duration)
		r.Results = append(r.Results, result)
		if snapshot != nil {
			if snapshotErr := snapshot(*r); snapshotErr != nil {
				return snapshotErr
			}
		}
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s users=%d rate=%d passed=%v startup=%v steady=%v RPS=%.1f p95=%.1fms p99=%.1fms errors=%d missed=%d journeys=%d\n", s.name, s.users, s.rate, result.Passed, result.StartupPassed, result.SteadyPassed, result.Steady.Overall.RPS, result.Steady.Overall.P95, result.Steady.Overall.P99, result.Steady.Overall.Errors, result.Missed, result.Journeys); err != nil {
			return err
		}
		if result.Verification != "passed" {
			return fmt.Errorf("%s verification failed: %s", s.name, result.Verification)
		}
		if !result.Passed {
			failedGroup = s.name
			if cfg.Mode == "check" || cfg.Smoke || (s.name == "study" && s.users == 50) {
				criteriaFailed = true
			}
		}
		// Always measure 100-user headroom, even when the 50-user target fails.
		if s.name == "study" {
			failedGroup = ""
		}
	}
	if criteriaFailed {
		return fmt.Errorf("performance criteria failed; see results")
	}
	return nil
}
