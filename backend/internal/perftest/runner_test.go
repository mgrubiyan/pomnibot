package perftest

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCriteria(t *testing.T) {
	for _, tc := range []struct {
		name, only, mode string
		smoke, pass      bool
	}{
		{"rate check", "rate", "check", false, false},
		{"concurrency check", "concurrency", "check", false, false},
		{"capacity exploration", "rate", "explore", false, true},
		{"required study target", "study", "explore", false, false},
		{"smoke exploration", "all", "explore", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultOptions()
			cfg.Only = tc.only
			cfg.Mode = tc.mode
			cfg.Smoke = tc.smoke
			r := Report{}
			calls := 0
			err := runStages(context.Background(), cfg, &r, io.Discard, nil, func(_ context.Context, _ Options, name string, n, rate int, _ time.Duration) (StageResult, error) {
				calls++
				return StageResult{Name: name, Users: n, Rate: rate, Verification: "passed"}, nil
			})
			if tc.pass {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			assert.Positive(t, calls)
		})
	}
}

func TestExplorationRejectsIncorrectData(t *testing.T) {
	cfg := DefaultOptions()
	cfg.Only = "rate"
	cfg.Mode = "explore"
	err := runStages(context.Background(), cfg, &Report{}, io.Discard, nil, func(context.Context, Options, string, int, int, time.Duration) (StageResult, error) {
		return StageResult{Verification: "history changed"}, nil
	})
	require.ErrorContains(t, err, "verification")
}

func TestSnapshotAndStageErrors(t *testing.T) {
	for _, snapshotFails := range []bool{false, true} {
		cfg := DefaultOptions()
		cfg.Smoke = true
		expected := errors.New("failed")
		r := Report{}
		err := runStages(context.Background(), cfg, &r, io.Discard, func(Report) error {
			if snapshotFails {
				return expected
			}
			return nil
		}, func(context.Context, Options, string, int, int, time.Duration) (StageResult, error) {
			if !snapshotFails {
				return StageResult{}, expected
			}
			return StageResult{Passed: true, Verification: "passed"}, nil
		})
		require.ErrorIs(t, err, expected)
		assert.Len(t, r.Results, 1)
	}
}

func TestOptionsValidation(t *testing.T) {
	require.NoError(t, DefaultOptions().Validate())
	for _, tc := range []struct {
		name   string
		change func(*Options)
	}{
		{"mode", func(c *Options) { c.Mode = "unknown" }},
		{"group", func(c *Options) { c.Only = "unknown" }},
		{"smoke group", func(c *Options) { c.Smoke = true; c.Only = "rate" }},
		{"smoke without answers", func(c *Options) { c.Smoke = true; c.Stage = c.Think }},
		{"study", func(c *Options) { c.Study = 0 }},
		{"stage", func(c *Options) { c.Stage = 0 }},
		{"study without answers", func(c *Options) { c.Study = c.Think }},
		{"think", func(c *Options) { c.Think = 0 }},
		{"timeout", func(c *Options) { c.Timeout = 0 }},
		{"deadline", func(c *Options) { c.Deadline = 0 }},
		{"warmup", func(c *Options) { c.Warmup = -time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultOptions()
			tc.change(&cfg)
			r, err := Run(t.Context(), cfg, io.Discard, nil)
			require.Error(t, err)
			assert.Equal(t, err.Error(), r.Error)
			assert.Empty(t, r.Results)
		})
	}
}

func TestRunContinuesStudyHeadroom(t *testing.T) {
	for _, mode := range []string{"check", "explore"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultOptions()
			cfg.Mode = mode
			cfg.Only = "study"
			r := Report{}
			err := runStages(t.Context(), cfg, &r, io.Discard, nil, func(_ context.Context, _ Options, name string, n, rate int, _ time.Duration) (StageResult, error) {
				return StageResult{Name: name, Users: n, Rate: rate, Passed: n == 50, Verification: "passed"}, nil
			})
			if mode == "check" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, r.Results, 2)
			assert.Equal(t, 100, r.Results[1].Users)
		})
	}
}

func TestAllStagesAndSnapshots(t *testing.T) {
	cfg := DefaultOptions()
	r := Report{}
	var snapshots []int
	err := runStages(t.Context(), cfg, &r, io.Discard, func(r Report) error {
		snapshots = append(snapshots, len(r.Results))
		return nil
	}, func(_ context.Context, _ Options, name string, n, rate int, _ time.Duration) (StageResult, error) {
		return StageResult{Name: name, Users: n, Rate: rate, Passed: true, Verification: "passed"}, nil
	})
	require.NoError(t, err)
	require.Len(t, r.Results, 14)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}, snapshots)
	assert.Equal(t, "study", r.Results[0].Name)
	assert.Equal(t, 400, r.Results[7].Users)
	assert.Equal(t, 800, r.Results[13].Rate)
}
