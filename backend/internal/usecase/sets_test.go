package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestSetService_UpdateSetTitle(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	otherUserID := uuid.New()
	setID := uuid.New()

	var pgSetUUID, pgAuthorUUID pgtype.UUID
	_ = pgSetUUID.Scan(setID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	t.Run("success by author", func(t *testing.T) {
		var updatedParams db.UpdateSetTitleParams
		mock := &mockQuerier{
			isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
				if arg.ID == pgSetUUID && arg.AuthorID == pgAuthorUUID {
					return true, nil
				}
				return false, nil
			},
			updateSetTitleFunc: func(_ context.Context, arg db.UpdateSetTitleParams) (int64, error) {
				updatedParams = arg
				return 1, nil
			},
		}

		svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
		err := svc.UpdateSetTitle(ctx, authorID, setID, "New Set Title")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if updatedParams.Title != "New Set Title" {
			t.Fatalf("expected title 'New Set Title', got %q", updatedParams.Title)
		}
		if updatedParams.ID != pgSetUUID || updatedParams.AuthorID != pgAuthorUUID {
			t.Fatalf("expected set id and author id to match, got %+v", updatedParams)
		}
	})

	t.Run("forbidden for non-author", func(t *testing.T) {
		mock := &mockQuerier{
			isSetAuthorFunc: func(_ context.Context, _ db.IsSetAuthorParams) (bool, error) {
				return false, nil
			},
			setExistsFunc: func(_ context.Context, _ pgtype.UUID) (bool, error) {
				return true, nil
			},
		}

		svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
		err := svc.UpdateSetTitle(ctx, otherUserID, setID, "New Title")
		if !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("not found when set does not exist", func(t *testing.T) {
		mock := &mockQuerier{
			isSetAuthorFunc: func(_ context.Context, _ db.IsSetAuthorParams) (bool, error) {
				return false, nil
			},
			setExistsFunc: func(_ context.Context, _ pgtype.UUID) (bool, error) {
				return false, nil
			},
		}

		svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
		err := svc.UpdateSetTitle(ctx, otherUserID, setID, "New Title")
		if !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("validation on empty title", func(t *testing.T) {
		mock := &mockQuerier{}
		svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
		err := svc.UpdateSetTitle(ctx, authorID, setID, "   ")
		if !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})
}

func TestSetService_GetCardsBySetID_StepReturned(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	setID := uuid.New()
	factID := uuid.New()
	card1 := uuid.New()
	card2 := uuid.New()

	var pgSetUUID, pgAuthorUUID, pgFactUUID pgtype.UUID
	_ = pgSetUUID.Scan(setID.String())
	_ = pgAuthorUUID.Scan(authorID.String())
	_ = pgFactUUID.Scan(factID.String())

	var pgCard1, pgCard2 pgtype.UUID
	_ = pgCard1.Scan(card1.String())
	_ = pgCard2.Scan(card2.String())

	mock := &mockQuerier{
		getSetByIDFunc: func(_ context.Context, _ db.GetSetByIDParams) (db.GetSetByIDRow, error) {
			return db.GetSetByIDRow{ID: pgSetUUID, AuthorID: pgAuthorUUID, Title: "Set 1"}, nil
		},
		getCardsBySetIDFunc: func(_ context.Context, _ db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error) {
			return []db.GetCardsBySetIDRow{
				{
					ID:       pgCard1,
					SetID:    pgSetUUID,
					FactID:   pgFactUUID,
					FactName: "Fact 1",
					Kind:     db.CardKindFlip,
					Question: "Q1",
					Step:     0,
				},
				{
					ID:       pgCard2,
					SetID:    pgSetUUID,
					FactID:   pgFactUUID,
					FactName: "Fact 1",
					Kind:     db.CardKindFlip,
					Question: "Q2",
					Step:     1,
				},
			}, nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
	cards, err := svc.GetCardsBySetID(ctx, authorID, setID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if !cards[0].Step.IsSet() || cards[0].Step.Value != 0 {
		t.Fatalf("expected card 0 step 0, got %+v", cards[0].Step)
	}
	if !cards[1].Step.IsSet() || cards[1].Step.Value != 1 {
		t.Fatalf("expected card 1 step 1, got %+v", cards[1].Step)
	}
}
