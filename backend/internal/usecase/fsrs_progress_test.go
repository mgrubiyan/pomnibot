package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	fsrs "github.com/open-spaced-repetition/go-fsrs"
)

type mockProgressQuerier struct {
	db.Querier
	getUserFactProgressFunc func(ctx context.Context, arg db.GetUserFactProgressParams) (db.UserFactProgress, error)
	countCardsByFactIDFunc  func(ctx context.Context, factID pgtype.UUID) (int32, error)
	upsertFactProgressFunc  func(ctx context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error)
}

func (m *mockProgressQuerier) GetUserFactProgress(ctx context.Context, arg db.GetUserFactProgressParams) (db.UserFactProgress, error) {
	if m.getUserFactProgressFunc != nil {
		return m.getUserFactProgressFunc(ctx, arg)
	}
	return db.UserFactProgress{}, nil
}

func (m *mockProgressQuerier) CountCardsByFactID(ctx context.Context, factID pgtype.UUID) (int32, error) {
	if m.countCardsByFactIDFunc != nil {
		return m.countCardsByFactIDFunc(ctx, factID)
	}
	return 1, nil
}

func (m *mockProgressQuerier) UpsertFactProgress(ctx context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error) {
	if m.upsertFactProgressFunc != nil {
		return m.upsertFactProgressFunc(ctx, arg)
	}
	return db.UserFactProgress{}, nil
}

func TestProcessFactAnswer_NewCard_Correct(t *testing.T) {
	ctx := context.Background()
	userID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	factID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	var upserted db.UpsertFactProgressParams
	mock := &mockProgressQuerier{
		getUserFactProgressFunc: func(_ context.Context, _ db.GetUserFactProgressParams) (db.UserFactProgress, error) {
			return db.UserFactProgress{}, pgx.ErrNoRows
		},
		countCardsByFactIDFunc: func(_ context.Context, _ pgtype.UUID) (int32, error) {
			return 3, nil
		},
		upsertFactProgressFunc: func(_ context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error) {
			upserted = arg
			return db.UserFactProgress{}, nil
		},
	}

	err := processFactAnswer(ctx, mock, userID, factID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if upserted.Step != 1 {
		t.Errorf("expected step 1, got %d", upserted.Step)
	}
	if upserted.Stability <= 0 {
		t.Errorf("expected positive stability, got %f", upserted.Stability)
	}
	if upserted.Difficulty <= 0 {
		t.Errorf("expected positive difficulty, got %f", upserted.Difficulty)
	}
	if upserted.Reps != 1 {
		t.Errorf("expected reps 1, got %d", upserted.Reps)
	}
	if upserted.Lapses != 0 {
		t.Errorf("expected lapses 0, got %d", upserted.Lapses)
	}
	if upserted.NextReviewAt.Time.Before(time.Now()) {
		t.Errorf("expected next review in future, got %v", upserted.NextReviewAt.Time)
	}
}

func TestProcessFactAnswer_ExistingCard_Incorrect(t *testing.T) {
	ctx := context.Background()
	userID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	factID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	var upserted db.UpsertFactProgressParams
	mock := &mockProgressQuerier{
		getUserFactProgressFunc: func(_ context.Context, _ db.GetUserFactProgressParams) (db.UserFactProgress, error) {
			return db.UserFactProgress{
				Step:           2,
				Stability:      3.5,
				Difficulty:     4.0,
				Reps:           2,
				Lapses:         0,
				State:          int16(fsrs.Review),
				LastReviewedAt: pgtype.Timestamptz{Time: time.Now().Add(-48 * time.Hour), Valid: true},
				NextReviewAt:   pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true},
			}, nil
		},
		countCardsByFactIDFunc: func(_ context.Context, _ pgtype.UUID) (int32, error) {
			return 3, nil
		},
		upsertFactProgressFunc: func(_ context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error) {
			upserted = arg
			return db.UserFactProgress{}, nil
		},
	}

	err := processFactAnswer(ctx, mock, userID, factID, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step rolled back from 2 to 1 on failure
	if upserted.Step != 1 {
		t.Errorf("expected step 1, got %d", upserted.Step)
	}
	if upserted.Lapses != 1 {
		t.Errorf("expected lapses 1, got %d", upserted.Lapses)
	}
	if upserted.Reps != 3 {
		t.Errorf("expected reps 3, got %d", upserted.Reps)
	}
}

func TestProcessFactAnswer_StepClampedToTotalCards(t *testing.T) {
	ctx := context.Background()
	userID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	factID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	var upserted db.UpsertFactProgressParams
	mock := &mockProgressQuerier{
		getUserFactProgressFunc: func(_ context.Context, _ db.GetUserFactProgressParams) (db.UserFactProgress, error) {
			return db.UserFactProgress{
				Step:           3,
				Stability:      10.0,
				Difficulty:     2.0,
				Reps:           5,
				Lapses:         0,
				State:          int16(fsrs.Review),
				LastReviewedAt: pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true},
			}, nil
		},
		countCardsByFactIDFunc: func(_ context.Context, _ pgtype.UUID) (int32, error) {
			return 3, nil // Total cards is 3
		},
		upsertFactProgressFunc: func(_ context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error) {
			upserted = arg
			return db.UserFactProgress{}, nil
		},
	}

	err := processFactAnswer(ctx, mock, userID, factID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step cannot exceed totalCards (3)
	if upserted.Step != 3 {
		t.Errorf("expected step clamped to 3, got %d", upserted.Step)
	}
}
