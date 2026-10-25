package usecase

import (
	"math"
	"reflect"
	"testing"
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs"
)

func TestRecallProbability(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	card := fsrs.Card{Stability: 2, State: fsrs.Review, LastReview: now.Add(-48 * time.Hour)}
	if got := recallProbability(card, now); math.Abs(got-0.9) > 1e-12 {
		t.Fatalf("recall after one stability: %v", got)
	}
	p := fsrs.DefaultParam()
	want := math.Pow(1+p.Factor*0.25/2, p.Decay)
	if got := recallProbability(card, card.LastReview.Add(6*time.Hour)); math.Abs(got-want) > 1e-12 {
		t.Fatalf("fractional days: got %v, want %v", got, want)
	}
	if recallProbability(fsrs.NewCard(), now) != 0 {
		t.Fatal("unlearned fact must have zero recall")
	}
}

func TestRetentionGroups(t *testing.T) {
	for _, tc := range []struct {
		probability float64
		group       int
	}{
		{0, 0}, {0.699999, 0}, {0.7, 1}, {0.9, 1}, {0.900001, 2}, {1, 2},
	} {
		if got := retentionGroup(tc.probability); got != tc.group {
			t.Errorf("probability %v: group %d, want %d", tc.probability, got, tc.group)
		}
	}
}

func TestRetentionEmptyAndNew(t *testing.T) {
	now := time.Now().UTC()
	empty := buildRetention(nil, now)
	if empty.TotalFacts != 0 || empty.Current != 0 || len(empty.Points) < 2 {
		t.Fatalf("empty: %+v", empty)
	}
	facts := []retentionFact{{card: fsrs.NewCard(), available: true}, {card: fsrs.NewCard(), available: false}}
	before := append([]retentionFact(nil), facts...)
	r := buildRetention(facts, now)
	if r.TotalFacts != 2 || r.Current != 0 || r.Groups.Below70 != 2 {
		t.Fatalf("new facts: %+v", r)
	}
	if r.Points[0].Day != 0 || r.Points[0].WithReviews != 0 || r.Points[0].WithoutReviews != 0 {
		t.Fatal("both curves must start before the initial review")
	}
	if r.Points[1].Day != 0 || r.Points[1].WithReviews != 0.5 {
		t.Fatal("only the available fact should receive an initial review")
	}
	foundIntraday := false
	for i, point := range r.Points {
		if point.WithoutReviews != 0 || point.WithReviews > 0.5 {
			t.Fatal("unavailable fact was reviewed")
		}
		if point.Day == 10.0/(24*60) && i > 0 && r.Points[i-1].Day == point.Day {
			foundIntraday = true
		}
	}
	if !foundIntraday {
		t.Fatal("missing FSRS learning review after ten minutes")
	}
	if !reflect.DeepEqual(before, facts) {
		t.Fatal("simulation mutated source state")
	}
}

func TestRetentionOverdueAndScheduled(t *testing.T) {
	now := time.Now().UTC()
	card := fsrs.Card{State: fsrs.Review, Stability: 2, Difficulty: 5, Reps: 2,
		LastReview: now.Add(-72 * time.Hour), Due: now.Add(-time.Hour)}
	r := buildRetention([]retentionFact{{card: card, available: true}, {card: card, available: true}}, now)
	if r.Points[1].Day != 0 || r.Points[1].WithReviews != 1 {
		t.Fatal("overdue facts should be reviewed together at forecast start")
	}
	p := fsrs.DefaultParam()
	next := p.Repeat(card, now)[fsrs.Good].Card
	wantDay := next.Due.Sub(now).Hours() / 24
	found := false
	for i := 1; i < len(r.Points); i++ {
		prev, point := r.Points[i-1], r.Points[i]
		if point.Day-prev.Day > 0.5+1e-10 {
			t.Fatal("sampling gap exceeds twelve hours")
		}
		if point.WithoutReviews > prev.WithoutReviews {
			t.Fatal("pause curve increases")
		}
		if point.Day == wantDay && prev.Day == point.Day && point.WithReviews > prev.WithReviews {
			found = true
		}
		if i > 1 && point.Day == prev.Day && prev.Day == r.Points[i-2].Day {
			t.Fatal("simultaneous events were not merged")
		}
	}
	if !found {
		t.Fatalf("missing next scheduled review at day %v", wantDay)
	}
	last := r.Points[len(r.Points)-1]
	if last.Day != 13 || last.WithReviews != r.WithReviews || last.WithoutReviews != r.WithoutReviews {
		t.Fatal("endpoints disagree")
	}
	card.Due = now.Add(3 * time.Hour)
	future := buildRetention([]retentionFact{{card: card, available: true}}, now)
	if future.Points[1].Day != 0.125 {
		t.Fatal("future review must occur exactly when due")
	}
}

func TestRetentionUnavailableLearnedFact(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	card := fsrs.Card{State: fsrs.Review, Stability: 2, Difficulty: 5, Reps: 2, LastReview: now.Add(-48 * time.Hour), Due: now.Add(-time.Hour)}
	facts := []retentionFact{{card: card, available: false}, {card: fsrs.NewCard(), available: false}}
	before := append([]retentionFact(nil), facts...)
	result := buildRetention(facts, now)
	if math.Abs(float64(result.Current)-0.45) > 1e-12 || result.Groups.Below70 != 1 || result.Groups.From70To90 != 1 {
		t.Fatalf("fact average and groups: %+v", result)
	}
	for _, point := range result.Points {
		if point.WithReviews != point.WithoutReviews {
			t.Fatal("unavailable learned fact received a simulated answer")
		}
	}
	if result.WithoutReviews >= result.Current {
		t.Fatal("learned fact should forget during the pause")
	}
	if !reflect.DeepEqual(before, facts) {
		t.Fatal("learned source state mutated")
	}
}

func TestRetentionReviewAtHorizon(t *testing.T) {
	now := time.Now().UTC()
	card := fsrs.Card{State: fsrs.Review, Stability: 15, Difficulty: 5, Reps: 2, LastReview: now.Add(-24 * time.Hour), Due: now.Add(13 * 24 * time.Hour)}
	result := buildRetention([]retentionFact{{card: card, available: true}}, now)
	end := result.Points[len(result.Points)-1]
	before := result.Points[len(result.Points)-2]
	if end.Day != 13 || before.Day != 13 || end.WithReviews != 1 || end.WithReviews != result.WithReviews {
		t.Fatal("review exactly at the horizon must be reflected in final values")
	}
}
