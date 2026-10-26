package perftest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStageFailure(t *testing.T) {
	for _, tc := range []struct {
		name, mode, group string
		users             int
		smoke, passed     bool
		verification      string
		wantError         bool
	}{
		{"successful check", "check", "rate", 100, false, true, "passed", false},
		{"failed rate check", "check", "rate", 100, false, false, "passed", true},
		{"failed concurrency check", "check", "concurrency", 10, false, false, "passed", true},
		{"capacity limit", "explore", "rate", 100, false, false, "passed", false},
		{"required study target", "explore", "study", 50, false, false, "passed", true},
		{"optional headroom", "explore", "study", 100, false, false, "passed", false},
		{"required smoke", "explore", "study", 2, true, false, "passed", true},
		{"incorrect data", "explore", "rate", 100, false, true, "history changed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := stageFailure(Options{Mode: tc.mode, Smoke: tc.smoke}, StageResult{Name: tc.group, Users: tc.users, Passed: tc.passed, Verification: tc.verification})
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
