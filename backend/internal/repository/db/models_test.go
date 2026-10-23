package db

import (
	"testing"
)

func TestCardKindValues(t *testing.T) {
	kinds := []CardKind{
		CardKindFlip,
		CardKindBoolean,
		CardKindChoice,
		CardKindTable,
		CardKindInput,
	}

	expected := []string{"flip", "boolean", "choice", "table", "input"}
	for i, k := range kinds {
		if string(k) != expected[i] {
			t.Errorf("kind %d = %s, want %s", i, k, expected[i])
		}
	}
}

// TestEloToStepMigration verifies how legacy elo values map into initial FSRS steps:
// < 800: step 0 (flip)
// 800-999: step 1 (boolean)
// 1000-1199: step 2 (choice)
// 1200-1399: step 3 (table)
// >= 1400: step 4 (input)
func TestEloToStepMigration(t *testing.T) {
	mapEloToStep := func(elo int32) int {
		switch {
		case elo < 800:
			return 0
		case elo < 1000:
			return 1
		case elo < 1200:
			return 2
		case elo < 1400:
			return 3
		default:
			return 4
		}
	}

	cases := []struct {
		elo      int32
		wantStep int
	}{
		{elo: 650, wantStep: 0},
		{elo: 700, wantStep: 0},
		{elo: 799, wantStep: 0},
		{elo: 800, wantStep: 1},
		{elo: 999, wantStep: 1},
		{elo: 1000, wantStep: 2},
		{elo: 1199, wantStep: 2},
		{elo: 1200, wantStep: 3},
		{elo: 1399, wantStep: 3},
		{elo: 1400, wantStep: 4},
		{elo: 1600, wantStep: 4},
	}

	for _, tc := range cases {
		got := mapEloToStep(tc.elo)
		if got != tc.wantStep {
			t.Errorf("mapEloToStep(%d) = %d, want %d", tc.elo, got, tc.wantStep)
		}
	}
}
