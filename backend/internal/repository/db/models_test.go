package db

import (
	"testing"
)

func TestCardIssueReason_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    CardIssueReason
		wantErr bool
	}{
		{
			name:    "valid string answer",
			input:   "answer",
			want:    CardIssueReasonAnswer,
			wantErr: false,
		},
		{
			name:    "valid bytes wording",
			input:   []byte("wording"),
			want:    CardIssueReasonWording,
			wantErr: false,
		},
		{
			name:    "valid string not-in-notes",
			input:   "not-in-notes",
			want:    CardIssueReasonNotInNotes,
			wantErr: false,
		},
		{
			name:    "valid string other",
			input:   "other",
			want:    CardIssueReasonOther,
			wantErr: false,
		},
		{
			name:    "invalid type int",
			input:   123,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r CardIssueReason
			err := r.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && r != tt.want {
				t.Errorf("Scan(%v) = %v, want %v", tt.input, r, tt.want)
			}
		})
	}
}

func TestNullCardIssueReason(t *testing.T) {
	var nullReason NullCardIssueReason
	if err := nullReason.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) unexpected error: %v", err)
	}
	if nullReason.Valid {
		t.Errorf("expected Valid to be false on nil scan")
	}

	val, err := nullReason.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}
	if val != nil {
		t.Errorf("expected Value() to be nil, got %v", val)
	}

	if err := nullReason.Scan("wording"); err != nil {
		t.Fatalf("Scan('wording') unexpected error: %v", err)
	}
	if !nullReason.Valid || nullReason.CardIssueReason != CardIssueReasonWording {
		t.Errorf("expected Valid true with wording, got %v (%s)", nullReason.Valid, nullReason.CardIssueReason)
	}

	val, err = nullReason.Value()
	if err != nil || val != "wording" {
		t.Errorf("expected Value() 'wording', got %v (err: %v)", val, err)
	}
}

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
