package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestCardService_CreateCardInFact(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	otherUserID := uuid.New()
	factID := uuid.New()

	var pgFactUUID, pgAuthorUUID pgtype.UUID
	_ = pgFactUUID.Scan(factID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	req := &contracts.CreateCardRequest{
		Kind:        contracts.CardKindFlip,
		Question:    "What is photosynthesis?",
		Answer:      contracts.NewStringCardAnswer("Process of converting light to chemical energy"),
		Explanation: contracts.NewOptString("Detailed explanation"),
	}

	t.Run("success by author increments step", func(t *testing.T) {
		var createdCardParams db.CreateCardParams
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return arg.ID == pgFactUUID && arg.AuthorID == pgAuthorUUID, nil
			},
			getMaxCardStepInFactFunc: func(_ context.Context, fid pgtype.UUID) (int32, error) {
				return 2, nil // Already has step 0, 1, 2
			},
			createCardFunc: func(_ context.Context, arg db.CreateCardParams) (db.Card, error) {
				createdCardParams = arg
				return db.Card{
					ID:          arg.ID,
					FactID:      arg.FactID,
					Kind:        arg.Kind,
					Question:    arg.Question,
					Step:        arg.Step,
					Explanation: arg.Explanation,
				}, nil
			},
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))
		card, err := svc.CreateCardInFact(ctx, authorID, factID, req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if card.Question != req.Question {
			t.Fatalf("expected question %q, got %q", req.Question, card.Question)
		}
		if card.FactId != factID {
			t.Fatalf("expected factId %v, got %v", factID, card.FactId)
		}
		if createdCardParams.Step != 3 {
			t.Fatalf("expected next step to be 3 (maxStep 2 + 1), got %d", createdCardParams.Step)
		}
	})

	t.Run("forbidden for non-author", func(t *testing.T) {
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return false, nil
			},
			factExistsFunc: func(_ context.Context, id pgtype.UUID) (bool, error) {
				return true, nil
			},
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))
		_, err := svc.CreateCardInFact(ctx, otherUserID, factID, req)
		if !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("validation on empty question", func(t *testing.T) {
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return true, nil
			},
		}

		badReq := &contracts.CreateCardRequest{
			Kind:     contracts.CardKindFlip,
			Question: "   ",
			Answer:   contracts.NewStringCardAnswer("Answer"),
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))
		_, err := svc.CreateCardInFact(ctx, authorID, factID, badReq)
		if !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("expected ErrValidation on bad question, got %v", err)
		}
	})
}
