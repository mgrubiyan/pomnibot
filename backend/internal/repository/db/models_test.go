package db

import (
	"testing"
)

// TestDifficultyBrackets verifies the business logic specification:
// 0: flip (< 800)
// 1: boolean (800-999)
// 2: choice (1000-1199)
// 3: table (1200-1399)
// 4: input (>= 1400)
func TestDifficultyBrackets(t *testing.T) {
	mapEloToDifficulty := func(elo int32) (int16, string) {
		switch {
		case elo < 800:
			return 0, "flip"
		case elo < 1000:
			return 1, "boolean"
		case elo < 1200:
			return 2, "choice"
		case elo < 1400:
			return 3, "table"
		default:
			return 4, "input"
		}
	}

	cases := []struct {
		elo          int32
		wantLevel    int16
		wantCardKind string
	}{
		{elo: 650, wantLevel: 0, wantCardKind: "flip"},
		{elo: 700, wantLevel: 0, wantCardKind: "flip"}, // default initial Elo
		{elo: 799, wantLevel: 0, wantCardKind: "flip"},
		{elo: 800, wantLevel: 1, wantCardKind: "boolean"},
		{elo: 999, wantLevel: 1, wantCardKind: "boolean"},
		{elo: 1000, wantLevel: 2, wantCardKind: "choice"},
		{elo: 1199, wantLevel: 2, wantCardKind: "choice"},
		{elo: 1200, wantLevel: 3, wantCardKind: "table"},
		{elo: 1399, wantLevel: 3, wantCardKind: "table"},
		{elo: 1400, wantLevel: 4, wantCardKind: "input"},
		{elo: 1600, wantLevel: 4, wantCardKind: "input"},
	}

	for _, tc := range cases {
		gotLevel, gotKind := mapEloToDifficulty(tc.elo)
		if gotLevel != tc.wantLevel || gotKind != tc.wantCardKind {
			t.Errorf("mapEloToDifficulty(%d) = (%d, %s), want (%d, %s)",
				tc.elo, gotLevel, gotKind, tc.wantLevel, tc.wantCardKind)
		}
	}
}
