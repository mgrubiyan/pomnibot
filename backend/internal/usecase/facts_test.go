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

func TestFactService_CreateFact(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	otherUserID := uuid.New()
	setID := uuid.New()

	var pgSetUUID, pgAuthorUUID pgtype.UUID
	_ = pgSetUUID.Scan(setID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	t.Run("success by author", func(t *testing.T) {
		var createdParams db.CreateFactParams
		mock := &mockQuerier{
			isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
				return arg.ID == pgSetUUID && arg.AuthorID == pgAuthorUUID, nil
			},
			createFactFunc: func(_ context.Context, arg db.CreateFactParams) (db.Fact, error) {
				createdParams = arg
				return db.Fact{
					ID:    arg.ID,
					SetID: arg.SetID,
					Name:  arg.Name,
				}, nil
			},
		}

		svc := usecase.NewFactService(mock)
		res, err := svc.CreateFact(ctx, authorID, setID, "New Fact")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if res.Name != "New Fact" {
			t.Fatalf("expected name 'New Fact', got %q", res.Name)
		}
		if res.SetId != setID {
			t.Fatalf("expected set id %v, got %v", setID, res.SetId)
		}
		if createdParams.Name != "New Fact" || createdParams.SetID != pgSetUUID {
			t.Fatalf("unexpected params sent to querier: %+v", createdParams)
		}
	})

	t.Run("forbidden for non-author", func(t *testing.T) {
		mock := &mockQuerier{
			isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
				return false, nil
			},
			setExistsFunc: func(_ context.Context, id pgtype.UUID) (bool, error) {
				return true, nil
			},
		}

		svc := usecase.NewFactService(mock)
		_, err := svc.CreateFact(ctx, otherUserID, setID, "New Fact")
		if !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("validation on empty name", func(t *testing.T) {
		mock := &mockQuerier{}
		svc := usecase.NewFactService(mock)
		_, err := svc.CreateFact(ctx, authorID, setID, "   ")
		if !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})
}

func TestFactService_UpdateFact(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	factID := uuid.New()

	var pgFactUUID, pgAuthorUUID pgtype.UUID
	_ = pgFactUUID.Scan(factID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	t.Run("success by author", func(t *testing.T) {
		var updatedParams db.UpdateFactParams
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return arg.ID == pgFactUUID && arg.AuthorID == pgAuthorUUID, nil
			},
			updateFactFunc: func(_ context.Context, arg db.UpdateFactParams) (db.Fact, error) {
				updatedParams = arg
				return db.Fact{ID: arg.ID, Name: arg.Name}, nil
			},
		}

		svc := usecase.NewFactService(mock)
		err := svc.UpdateFact(ctx, authorID, factID, "Renamed Fact")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if updatedParams.Name != "Renamed Fact" || updatedParams.ID != pgFactUUID {
			t.Fatalf("unexpected params: %+v", updatedParams)
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

		svc := usecase.NewFactService(mock)
		err := svc.UpdateFact(ctx, authorID, factID, "Renamed Fact")
		if !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})
}

func TestFactService_DeleteFact(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	factID := uuid.New()

	var pgFactUUID, pgAuthorUUID pgtype.UUID
	_ = pgFactUUID.Scan(factID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	t.Run("success by author", func(t *testing.T) {
		deleted := false
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return arg.ID == pgFactUUID && arg.AuthorID == pgAuthorUUID, nil
			},
			deleteFactFunc: func(_ context.Context, id pgtype.UUID) (int64, error) {
				if id == pgFactUUID {
					deleted = true
				}
				return 1, nil
			},
		}

		svc := usecase.NewFactService(mock)
		err := svc.DeleteFact(ctx, authorID, factID)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !deleted {
			t.Fatalf("expected fact to be deleted")
		}
	})
}

func TestFactService_ReorderCards(t *testing.T) {
	ctx := context.Background()
	authorID := uuid.New()
	factID := uuid.New()

	card1 := uuid.New()
	card2 := uuid.New()
	card3 := uuid.New()

	var pgFactUUID, pgAuthorUUID pgtype.UUID
	_ = pgFactUUID.Scan(factID.String())
	_ = pgAuthorUUID.Scan(authorID.String())

	var pgCard1, pgCard2, pgCard3 pgtype.UUID
	_ = pgCard1.Scan(card1.String())
	_ = pgCard2.Scan(card2.String())
	_ = pgCard3.Scan(card3.String())

	t.Run("success reordering", func(t *testing.T) {
		steps := make(map[uuid.UUID]int32)
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return arg.ID == pgFactUUID && arg.AuthorID == pgAuthorUUID, nil
			},
			getCardsByFactIDFunc: func(_ context.Context, factID pgtype.UUID) ([]db.GetCardsByFactIDRow, error) {
				return []db.GetCardsByFactIDRow{
					{ID: pgCard1, Step: 0},
					{ID: pgCard2, Step: 1},
					{ID: pgCard3, Step: 2},
				}, nil
			},
			updateCardStepFunc: func(_ context.Context, arg db.UpdateCardStepParams) (int64, error) {
				var id uuid.UUID
				_ = id.Scan(arg.ID.Bytes[:])
				steps[id] = arg.Step
				return 1, nil
			},
		}

		svc := usecase.NewFactService(mock)
		// Reorder to card3 (0), card1 (1), card2 (2)
		newOrder := []uuid.UUID{card3, card1, card2}
		err := svc.ReorderCards(ctx, authorID, factID, newOrder)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if steps[card3] != 0 || steps[card1] != 1 || steps[card2] != 2 {
			t.Fatalf("unexpected step assignments: %+v", steps)
		}
	})

	t.Run("card not belonging to fact", func(t *testing.T) {
		unrelatedCard := uuid.New()
		mock := &mockQuerier{
			isFactAuthorFunc: func(_ context.Context, arg db.IsFactAuthorParams) (bool, error) {
				return true, nil
			},
			getCardsByFactIDFunc: func(_ context.Context, factID pgtype.UUID) ([]db.GetCardsByFactIDRow, error) {
				return []db.GetCardsByFactIDRow{
					{ID: pgCard1, Step: 0},
					{ID: pgCard2, Step: 1},
				}, nil
			},
		}

		svc := usecase.NewFactService(mock)
		err := svc.ReorderCards(ctx, authorID, factID, []uuid.UUID{pgCard1.Bytes, unrelatedCard})
		if !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("expected ErrValidation for card outside fact, got %v", err)
		}
	})
}
