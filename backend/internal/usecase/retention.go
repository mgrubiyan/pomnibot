package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	fsrs "github.com/open-spaced-repetition/go-fsrs"
)

const (
	retentionDays              = 13
	retentionSampleInterval    = 12 * time.Hour
	retentionEventsPerInterval = 2
	retentionMaxPoints         = 2 * (1 + retentionDays*2*(1+retentionEventsPerInterval))
)

type retentionFact struct {
	card      fsrs.Card
	available bool
}

// recallProbability uses the forgetting curve from go-fsrs v1.2.1. Unlike
// Repeat's scheduling, display samples preserve fractional elapsed days.
func recallProbability(card fsrs.Card, at time.Time) float64 {
	if card.State == fsrs.New || card.Stability <= 0 || card.LastReview.IsZero() {
		return 0
	}
	p := factFSRSParameters()
	days := math.Max(0, at.Sub(card.LastReview).Hours()/24)
	return math.Pow(1+p.Factor*days/card.Stability, p.Decay)
}

func retentionGroup(probability float64) int {
	switch {
	case probability < 0.7:
		return 0
	case probability <= 0.9:
		return 1
	default:
		return 2
	}
}

func averageRecall(ctx context.Context, facts []retentionFact, at time.Time) (float64, error) {
	if len(facts) == 0 {
		return 0, ctx.Err()
	}
	sum := 0.0
	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		sum += recallProbability(fact.card, at)
	}
	return sum / float64(len(facts)), nil
}

// buildRetention advances copies only. Display samples use a fixed half-day
// grid plus at most two event times per interval. Reviews between samples are
// still replayed at their exact due times, independently for each card. This
// bounds full-set scans and response size regardless of distinct review times:
// O(len(facts)*retentionMaxPoints + reviews), rather than O(len(facts)*reviews).
func buildRetention(ctx context.Context, facts []retentionFact, now time.Time) (*contracts.SetRetention, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := averageRecall(ctx, facts, now)
	if err != nil {
		return nil, err
	}
	simulated := append([]retentionFact(nil), facts...)
	result := &contracts.SetRetention{
		CalculatedAt: now, HorizonDays: retentionDays, Target: 0.9,
		TotalFacts: len(facts), Current: contracts.RecallProbability(current),
		Points: make([]contracts.RetentionPoint, 0, retentionMaxPoints),
	}
	for i, fact := range facts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if simulated[i].card.Due.Before(now) {
			simulated[i].card.Due = now
		}
		switch retentionGroup(recallProbability(fact.card, now)) {
		case 0:
			result.Groups.Below70++
		case 1:
			result.Groups.From70To90++
		case 2:
			result.Groups.Above90++
		}
	}
	sample := func(at time.Time) error {
		with, err := averageRecall(ctx, simulated, at)
		if err != nil {
			return err
		}
		without, err := averageRecall(ctx, facts, at)
		if err != nil {
			return err
		}
		result.Points = append(result.Points, contracts.RetentionPoint{
			Day:         at.Sub(now).Hours() / 24,
			WithReviews: contracts.RecallProbability(with), WithoutReviews: contracts.RecallProbability(without),
		})
		return nil
	}
	end := now.Add(retentionDays * 24 * time.Hour)
	at, nextGrid := now, now
	sampledEvents := 0
	p := factFSRSParameters()
	for {
		// Once an interval's event budget is exhausted, advance each card to
		// the next grid time without scanning the full set at skipped events.
		for i := range simulated {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fact := &simulated[i]
			for fact.available && fact.card.Due.Before(at) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				fact.card = p.Repeat(fact.card, fact.card.Due)[fsrs.Good].Card
			}
		}
		// Preserve before/after pairs at sampled events, including now/end.
		if err := sample(at); err != nil {
			return nil, err
		}
		reviewed := false
		for i := range simulated {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fact := &simulated[i]
			if fact.available && fact.card.Due.Equal(at) {
				fact.card = p.Repeat(fact.card, at)[fsrs.Good].Card
				reviewed = true
			}
		}
		if reviewed {
			if err := sample(at); err != nil {
				return nil, err
			}
		}
		if at.Equal(end) {
			break
		}
		if at.Equal(nextGrid) {
			nextGrid = minTime(at.Add(retentionSampleInterval), end)
			sampledEvents = 0
		}
		next := nextGrid
		if sampledEvents < retentionEventsPerInterval {
			for _, fact := range simulated {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if fact.available && fact.card.Due.After(at) && fact.card.Due.Before(next) {
					next = fact.card.Due
				}
			}
			if next.Before(nextGrid) {
				sampledEvents++
			}
		}
		at = next
	}
	last := result.Points[len(result.Points)-1]
	result.WithReviews, result.WithoutReviews = last.WithReviews, last.WithoutReviews
	return result, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *setServiceImpl) GetSetRetention(ctx context.Context, userID, setID uuid.UUID) (*contracts.SetRetention, error) {
	pgUser, pgSet := pgtype.UUID{Bytes: userID, Valid: true}, pgtype.UUID{Bytes: setID, Valid: true}
	// The same access query permits authors and current members, and hides
	// absent sets and other readers' sets behind the same 404.
	_, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{ID: pgSet, UserID: pgUser})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get retention set: %w", err)
	}
	rows, err := s.querier.GetSetRetentionFacts(ctx, db.GetSetRetentionFactsParams{SetID: pgSet, UserID: pgUser})
	if err != nil {
		return nil, fmt.Errorf("get retention facts: %w", err)
	}
	now := time.Now().UTC()
	facts := make([]retentionFact, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		card := fsrs.Card{Stability: row.Stability, Difficulty: row.Difficulty,
			Reps: uint64(row.Reps), Lapses: uint64(row.Lapses), State: fsrs.State(row.State)}
		if row.LastReviewedAt.Valid {
			card.LastReview = row.LastReviewedAt.Time
		}
		if row.NextReviewAt.Valid {
			card.Due = row.NextReviewAt.Time
		}
		facts = append(facts, retentionFact{card: card, available: row.Available})
	}
	return buildRetention(ctx, facts, now)
}
