package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
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
	empty := requireRetention(t, nil, now)
	if empty.TotalFacts != 0 || empty.Current != 0 || len(empty.Points) < 2 {
		t.Fatalf("empty: %+v", empty)
	}
	facts := []retentionFact{{card: fsrs.NewCard(), available: true}, {card: fsrs.NewCard(), available: false}}
	before := append([]retentionFact(nil), facts...)
	r := requireRetention(t, facts, now)
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
	r := requireRetention(t, []retentionFact{{card: card, available: true}, {card: card, available: true}}, now)
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
	future := requireRetention(t, []retentionFact{{card: card, available: true}}, now)
	if future.Points[1].Day != 0.125 {
		t.Fatal("future review must occur exactly when due")
	}
}

func TestRetentionUnavailableLearnedFact(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	card := fsrs.Card{State: fsrs.Review, Stability: 2, Difficulty: 5, Reps: 2, LastReview: now.Add(-48 * time.Hour), Due: now.Add(-time.Hour)}
	facts := []retentionFact{{card: card, available: false}, {card: fsrs.NewCard(), available: false}}
	before := append([]retentionFact(nil), facts...)
	result := requireRetention(t, facts, now)
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
	result := requireRetention(t, []retentionFact{{card: card, available: true}}, now)
	end := result.Points[len(result.Points)-1]
	before := result.Points[len(result.Points)-2]
	if end.Day != 13 || before.Day != 13 || end.WithReviews != 1 || end.WithReviews != result.WithReviews {
		t.Fatal("review exactly at the horizon must be reflected in final values")
	}
}

func TestRetentionSamplingIsBounded(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, span := range []time.Duration{12 * time.Hour, 13 * 24 * time.Hour} {
		t.Run(span.String(), func(t *testing.T) {
			facts := staggeredRetentionFacts(1000, now)
			for i := range facts {
				facts[i].card.Due = now.Add(time.Duration(i+1) * (span / time.Duration(len(facts)+1)))
			}
			result := requireRetention(t, facts, now)
			// 27 grid times plus at most two extra event times in each of the
			// 26 half-day intervals, each with at most a before/after pair.
			const maxPoints = 2 * (27 + 2*26)
			if len(result.Points) > maxPoints {
				t.Fatalf("got %d display points, limit is %d", len(result.Points), maxPoints)
			}
			for i := 0; i <= 26; i++ {
				day := float64(i) / 2
				found := false
				for _, point := range result.Points {
					if point.Day == day {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing fixed grid sample at day %v", day)
				}
			}
		})
	}
}

func TestRetentionCancellation(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	facts := staggeredRetentionFacts(1000, now)
	for _, after := range []int{1, 500, 3000, 10000, 20000} {
		t.Run(fmt.Sprintf("cancel_after_%d_checks", after), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			counted := &retentionCancelContext{Context: ctx, cancel: cancel, remaining: after}
			result, err := buildRetention(counted, facts, now)
			if result != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled forecast: result=%+v, err=%v", result, err)
			}
			if counted.remaining != 0 {
				t.Fatal("forecast continued checking context after cancellation")
			}
		})
	}
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), now.Add(-100*365*24*time.Hour))
		defer cancel()
		result, err := buildRetention(ctx, facts, now)
		if result != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired forecast: result=%+v, err=%v", result, err)
		}
	})
}

// Cancel during CPU work deterministically, without sleeps or timing assertions.
type retentionCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *retentionCancelContext) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestRetentionSamplesMatchExactSimulation(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	facts := staggeredRetentionFacts(300, now)
	for i := range facts {
		facts[i].card.Stability = 0.5 + float64(i%20)
		facts[i].card.Due = now.Add(time.Duration(i-10) * time.Hour)
		facts[i].available = i%7 != 0
		if i%11 == 0 {
			facts[i].card = fsrs.NewCard()
		}
	}
	// Exercise a review exactly at the horizon as well as skipped events.
	facts[len(facts)-1].card.Due = now.Add(retentionDays * 24 * time.Hour)
	before := append([]retentionFact(nil), facts...)
	result := requireRetention(t, facts, now)
	for i, point := range result.Points {
		at := now.Add(time.Duration(math.Round(point.Day * float64(24*time.Hour))))
		inclusive := i == len(result.Points)-1 || result.Points[i+1].Day != point.Day
		with, without := exactRetentionAt(facts, now, at, inclusive)
		if math.Abs(float64(point.WithReviews)-with) > 1e-12 || math.Abs(float64(point.WithoutReviews)-without) > 1e-12 {
			t.Fatalf("point %d at day %v: got (%v, %v), want (%v, %v)",
				i, point.Day, point.WithReviews, point.WithoutReviews, with, without)
		}
		if i > 0 && (point.Day < result.Points[i-1].Day || point.Day-result.Points[i-1].Day > 0.5+1e-12) {
			t.Fatalf("unordered samples or gap over twelve hours at point %d", i)
		}
	}
	last := result.Points[len(result.Points)-1]
	if result.Points[0].Day != 0 || last.Day != retentionDays || result.WithReviews != last.WithReviews || result.WithoutReviews != last.WithoutReviews {
		t.Fatal("forecast endpoints disagree")
	}
	if !reflect.DeepEqual(before, facts) {
		t.Fatal("simulation mutated source state")
	}
}

// Independently replay each card from the source at every output time. Display
// sampling may omit events, but it must never omit or round a scheduled review.
func exactRetentionAt(facts []retentionFact, now, at time.Time, inclusive bool) (float64, float64) {
	p := fsrs.DefaultParam()
	with, without := 0.0, 0.0
	for _, fact := range facts {
		without += recallProbability(fact.card, at)
		card := fact.card
		if card.Due.Before(now) {
			card.Due = now
		}
		for fact.available && (card.Due.Before(at) || inclusive && card.Due.Equal(at)) {
			card = p.Repeat(card, card.Due)[fsrs.Good].Card
		}
		with += recallProbability(card, at)
	}
	return with / float64(len(facts)), without / float64(len(facts))
}

func requireRetention(t *testing.T, facts []retentionFact, now time.Time) *contracts.SetRetention {
	t.Helper()
	result, err := buildRetention(t.Context(), facts, now)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
