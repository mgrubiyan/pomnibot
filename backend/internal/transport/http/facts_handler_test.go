package http

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockFactService struct {
	usecase.FactService
	getFactsBySetIDFunc func(ctx context.Context, userID, setID uuid.UUID) ([]contracts.Fact, error)
	createFactFunc      func(ctx context.Context, userID, setID uuid.UUID, name string) (*contracts.Fact, error)
	updateFactFunc      func(ctx context.Context, userID, factID uuid.UUID, name string) error
	deleteFactFunc      func(ctx context.Context, userID, factID uuid.UUID) error
	reorderCardsFunc    func(ctx context.Context, userID, factID uuid.UUID, cardIDs []uuid.UUID) error
}

func (m *mockFactService) GetFactsBySetID(ctx context.Context, userID, setID uuid.UUID) ([]contracts.Fact, error) {
	if m.getFactsBySetIDFunc != nil {
		return m.getFactsBySetIDFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockFactService) CreateFact(ctx context.Context, userID, setID uuid.UUID, name string) (*contracts.Fact, error) {
	if m.createFactFunc != nil {
		return m.createFactFunc(ctx, userID, setID, name)
	}
	return nil, nil
}

func (m *mockFactService) UpdateFact(ctx context.Context, userID, factID uuid.UUID, name string) error {
	if m.updateFactFunc != nil {
		return m.updateFactFunc(ctx, userID, factID, name)
	}
	return nil
}

func (m *mockFactService) DeleteFact(ctx context.Context, userID, factID uuid.UUID) error {
	if m.deleteFactFunc != nil {
		return m.deleteFactFunc(ctx, userID, factID)
	}
	return nil
}

func (m *mockFactService) ReorderCards(ctx context.Context, userID, factID uuid.UUID, cardIDs []uuid.UUID) error {
	if m.reorderCardsFunc != nil {
		return m.reorderCardsFunc(ctx, userID, factID, cardIDs)
	}
	return nil
}

func TestFactsHandler_CreateFact(t *testing.T) {
	userID := uuid.New()
	setID := uuid.New()
	req := &contracts.CreateFactRequest{Name: "New Fact"}

	t.Run("Success returns Fact", func(t *testing.T) {
		factID := uuid.New()
		svc := &mockFactService{
			createFactFunc: func(_ context.Context, uid, sid uuid.UUID, name string) (*contracts.Fact, error) {
				return &contracts.Fact{ID: factID, SetId: sid, Name: name}, nil
			},
		}
		h := NewFactsHandler(svc)
		res, err := h.CreateFact(WithUserID(context.Background(), userID), req, contracts.CreateFactParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fact, ok := res.(*contracts.Fact)
		if !ok || fact.ID != factID || fact.Name != "New Fact" {
			t.Fatalf("expected fact, got %+v", res)
		}
	})

	t.Run("Forbidden returns Forbidden", func(t *testing.T) {
		svc := &mockFactService{
			createFactFunc: func(_ context.Context, _, _ uuid.UUID, _ string) (*contracts.Fact, error) {
				return nil, usecase.ErrForbidden
			},
		}
		h := NewFactsHandler(svc)
		res, err := h.CreateFact(context.Background(), req, contracts.CreateFactParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.CreateFactForbidden); !ok {
			t.Fatalf("expected CreateFactForbidden, got %T", res)
		}
	})
}

func TestFactsHandler_ReorderCards(t *testing.T) {
	userID := uuid.New()
	factID := uuid.New()
	c1, c2 := uuid.New(), uuid.New()
	req := &contracts.ReorderCardsRequest{CardIds: []uuid.UUID{c2, c1}}

	t.Run("Success returns NoContent", func(t *testing.T) {
		called := false
		svc := &mockFactService{
			reorderCardsFunc: func(_ context.Context, uid, fid uuid.UUID, cardIDs []uuid.UUID) error {
				if uid == userID && fid == factID && len(cardIDs) == 2 && cardIDs[0] == c2 {
					called = true
				}
				return nil
			},
		}
		h := NewFactsHandler(svc)
		res, err := h.ReorderCards(WithUserID(context.Background(), userID), req, contracts.ReorderCardsParams{FactId: factID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.ReorderCardsNoContent); !ok {
			t.Fatalf("expected ReorderCardsNoContent, got %T", res)
		}
		if !called {
			t.Fatal("expected reorderCardsFunc to be called")
		}
	})
}
