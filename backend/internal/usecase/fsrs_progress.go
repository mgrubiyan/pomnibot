package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	fsrs "github.com/open-spaced-repetition/go-fsrs"
)

func processFactAnswer(ctx context.Context, q db.Querier, userID, factID pgtype.UUID, isCorrect bool) error {
	now := time.Now()

	var card fsrs.Card
	var currentStep int32

	p, err := q.GetUserFactProgress(ctx, db.GetUserFactProgressParams{
		UserID: userID,
		FactID: factID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get user fact progress: %w", err)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		card = fsrs.NewCard()
	} else {
		card = fsrs.Card{
			Due:        p.NextReviewAt.Time,
			Stability:  p.Stability,
			Difficulty: p.Difficulty,
			Reps:       uint64(p.Reps),
			Lapses:     uint64(p.Lapses),
			State:      fsrs.State(p.State),
		}
		if p.LastReviewedAt.Valid {
			card.LastReview = p.LastReviewedAt.Time
		}
		currentStep = p.Step
	}

	params := factFSRSParameters()
	scheduling := params.Repeat(card, now)
	rating := fsrs.Again
	if isCorrect {
		rating = fsrs.Good
	}
	nextCard := scheduling[rating].Card

	totalCards, err := q.CountCardsByFactID(ctx, factID)
	if err != nil {
		return fmt.Errorf("count cards by fact: %w", err)
	}
	if totalCards < 1 {
		totalCards = 1
	}

	var nextStep int32
	if isCorrect {
		nextStep = min(currentStep+1, totalCards)
	} else {
		nextStep = max(0, currentStep-1)
	}

	_, err = q.UpsertFactProgress(ctx, db.UpsertFactProgressParams{
		UserID:         userID,
		FactID:         factID,
		Step:           nextStep,
		Stability:      nextCard.Stability,
		Difficulty:     nextCard.Difficulty,
		Reps:           int32(nextCard.Reps),
		Lapses:         int32(nextCard.Lapses),
		State:          int16(nextCard.State),
		LastReviewedAt: pgtype.Timestamptz{Time: now, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: nextCard.Due, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("upsert fact progress: %w", err)
	}

	return nil
}

// factFSRSParameters is shared by real answers and retention forecasts.
func factFSRSParameters() fsrs.Parameters {
	return fsrs.DefaultParam()
}
