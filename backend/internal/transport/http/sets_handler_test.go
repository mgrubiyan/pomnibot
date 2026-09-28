package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockSetService struct {
	getSetFunc             func(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	deleteSetFunc          func(ctx context.Context, userID int64, setID uuid.UUID) error
	getCardsBySetIDFunc    func(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error)
	getSetShareCodeFunc    func(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	getSetPlanFunc         func(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error)
	joinSetByShareCodeFunc func(ctx context.Context, userID int64, code string) (*contracts.CardSet, error)
	generateMockSetFunc    func(ctx context.Context, userID int64, title string) (*contracts.CardSet, error)
}

func (m *mockSetService) GetSet(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error) {
	if m.getSetFunc != nil {
		return m.getSetFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockSetService) DeleteSet(ctx context.Context, userID int64, setID uuid.UUID) error {
	if m.deleteSetFunc != nil {
		return m.deleteSetFunc(ctx, userID, setID)
	}
	return nil
}

func (m *mockSetService) GetCardsBySetID(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error) {
	if m.getCardsBySetIDFunc != nil {
		return m.getCardsBySetIDFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockSetService) GetSetShareCode(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error) {
	if m.getSetShareCodeFunc != nil {
		return m.getSetShareCodeFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockSetService) GetSetPlan(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error) {
	if m.getSetPlanFunc != nil {
		return m.getSetPlanFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockSetService) JoinSetByShareCode(ctx context.Context, userID int64, code string) (*contracts.CardSet, error) {
	if m.joinSetByShareCodeFunc != nil {
		return m.joinSetByShareCodeFunc(ctx, userID, code)
	}
	return nil, nil
}

func (m *mockSetService) GenerateMockSet(ctx context.Context, userID int64, title string) (*contracts.CardSet, error) {
	if m.generateMockSetFunc != nil {
		return m.generateMockSetFunc(ctx, userID, title)
	}
	return nil, nil
}

func TestSetsHandler_GetSet(t *testing.T) {
	setID := uuid.New()
	expectedSet := &contracts.CardSet{
		ID:         setID,
		Title:      "Math 101",
		CardsTotal: 10,
		CardsDue:   3,
		Author: contracts.User{
			ID:        12345,
			FirstName: "Author",
		},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		setID        uuid.UUID
		mockSetup    func(t *testing.T) *mockSetService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.GetSetRes)
	}{
		{
			name:  "Success returns CardSet and propagates userID",
			ctx:   WithUserID(context.Background(), 12345),
			setID: setID,
			mockSetup: func(t *testing.T) *mockSetService {
				return &mockSetService{
					getSetFunc: func(_ context.Context, userID int64, id uuid.UUID) (*contracts.CardSet, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != setID {
							t.Fatalf("expected setID %s, got %s", setID, id)
						}
						return expectedSet, nil
					},
				}
			},
			wantResType: (*contracts.CardSet)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetSetRes) {
				cs, ok := res.(*contracts.CardSet)
				if !ok {
					t.Fatalf("expected *contracts.CardSet, got %T", res)
				}
				if cs.ID != setID || cs.Title != "Math 101" {
					t.Errorf("unexpected CardSet content: %+v", cs)
				}
			},
		},
		{
			name:  "Context without UserID falls back to 0",
			ctx:   context.Background(),
			setID: setID,
			mockSetup: func(t *testing.T) *mockSetService {
				return &mockSetService{
					getSetFunc: func(_ context.Context, userID int64, _ uuid.UUID) (*contracts.CardSet, error) {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return expectedSet, nil
					},
				}
			},
			wantResType: (*contracts.CardSet)(nil),
		},
		{
			name:  "Not Found error maps to GetSetNotFound",
			ctx:   context.Background(),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					getSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
						return nil, usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.GetSetNotFound)(nil),
		},
		{
			name:  "Forbidden error maps to GetSetNotFound to avoid leaking resource existence",
			ctx:   WithUserID(context.Background(), 99999),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					getSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
						return nil, usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.GetSetNotFound)(nil),
		},
		{
			name:  "Internal error maps to GetSetInternalServerError",
			ctx:   context.Background(),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					getSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
						return nil, errors.New("database connection failed")
					},
				}
			},
			wantResType: (*contracts.GetSetInternalServerError)(nil),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mockSetup(t)
			h := NewSetsHandler(svc)
			res, err := h.GetSet(tc.ctx, contracts.GetSetParams{SetId: tc.setID})
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			switch tc.wantResType.(type) {
			case *contracts.CardSet:
				if _, ok := res.(*contracts.CardSet); !ok {
					t.Fatalf("expected *contracts.CardSet, got %T", res)
				}
			case *contracts.GetSetNotFound:
				if _, ok := res.(*contracts.GetSetNotFound); !ok {
					t.Fatalf("expected *contracts.GetSetNotFound, got %T", res)
				}
			case *contracts.GetSetInternalServerError:
				if _, ok := res.(*contracts.GetSetInternalServerError); !ok {
					t.Fatalf("expected *contracts.GetSetInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unhandled expected response type: %T", tc.wantResType)
			}
			if tc.checkResFunc != nil {
				tc.checkResFunc(t, res)
			}
		})
	}
}

func TestSetsHandler_DeleteSet(t *testing.T) {
	setID := uuid.New()

	tests := []struct {
		name        string
		ctx         context.Context
		setID       uuid.UUID
		mockSetup   func(t *testing.T) *mockSetService
		wantResType any
	}{
		{
			name:  "Success returns DeleteSetNoContent",
			ctx:   WithUserID(context.Background(), 12345),
			setID: setID,
			mockSetup: func(t *testing.T) *mockSetService {
				return &mockSetService{
					deleteSetFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != setID {
							t.Fatalf("expected setID %s, got %s", setID, id)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.DeleteSetNoContent)(nil),
		},
		{
			name:  "Not Found returns DeleteSetNotFound",
			ctx:   context.Background(),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					deleteSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) error {
						return usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.DeleteSetNotFound)(nil),
		},
		{
			name:  "Forbidden returns DeleteSetNotFound to prevent information leakage",
			ctx:   WithUserID(context.Background(), 88888),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					deleteSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) error {
						return usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.DeleteSetNotFound)(nil),
		},
		{
			name:  "Internal error returns DeleteSetInternalServerError",
			ctx:   context.Background(),
			setID: setID,
			mockSetup: func(_ *testing.T) *mockSetService {
				return &mockSetService{
					deleteSetFunc: func(_ context.Context, _ int64, _ uuid.UUID) error {
						return errors.New("sql execution failed")
					},
				}
			},
			wantResType: (*contracts.DeleteSetInternalServerError)(nil),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mockSetup(t)
			h := NewSetsHandler(svc)
			res, err := h.DeleteSet(tc.ctx, contracts.DeleteSetParams{SetId: tc.setID})
			if err != nil {
				t.Fatalf("unexpected handler error: %v", err)
			}
			switch tc.wantResType.(type) {
			case *contracts.DeleteSetNoContent:
				if _, ok := res.(*contracts.DeleteSetNoContent); !ok {
					t.Fatalf("expected *contracts.DeleteSetNoContent, got %T", res)
				}
			case *contracts.DeleteSetNotFound:
				if _, ok := res.(*contracts.DeleteSetNotFound); !ok {
					t.Fatalf("expected *contracts.DeleteSetNotFound, got %T", res)
				}
			case *contracts.DeleteSetInternalServerError:
				if _, ok := res.(*contracts.DeleteSetInternalServerError); !ok {
					t.Fatalf("expected *contracts.DeleteSetInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unhandled expected response type: %T", tc.wantResType)
			}
		})
	}
}

func TestSetsHandler_GetCardsBySetID(t *testing.T) {
	targetSetID := uuid.New()
	otherSetID := uuid.New()

	cardInTarget := contracts.Card{
		ID:       uuid.New(),
		SetId:    targetSetID,
		Kind:     contracts.CardKindFlip,
		Question: "Target Question",
	}

	t.Run("Success returns cards for requested set", func(t *testing.T) {
		svc := &mockSetService{
			getCardsBySetIDFunc: func(_ context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error) {
				if userID != 42 {
					t.Fatalf("expected userID 42, got %d", userID)
				}
				if setID != targetSetID {
					t.Fatalf("expected targetSetID, got %s", setID)
				}
				return []contracts.Card{cardInTarget}, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetCardsBySetID(WithUserID(context.Background(), 42), contracts.GetCardsBySetIDParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cardsRes, ok := res.(*contracts.GetCardsBySetIDOKApplicationJSON)
		if !ok {
			t.Fatalf("expected *contracts.GetCardsBySetIDOKApplicationJSON, got %T", res)
		}
		if len(*cardsRes) != 1 || (*cardsRes)[0].ID != cardInTarget.ID {
			t.Fatalf("unexpected cards returned: %+v", *cardsRes)
		}
	})

	t.Run("Relational boundary: query for other set never leaks target set cards", func(t *testing.T) {
		svc := &mockSetService{
			getCardsBySetIDFunc: func(_ context.Context, _ int64, setID uuid.UUID) ([]contracts.Card, error) {
				if setID != targetSetID {
					return nil, usecase.ErrNotFound
				}
				return []contracts.Card{cardInTarget}, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetCardsBySetID(context.Background(), contracts.GetCardsBySetIDParams{SetId: otherSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetCardsBySetIDNotFound); !ok {
			t.Fatalf("expected *contracts.GetCardsBySetIDNotFound, got %T", res)
		}
	})

	t.Run("Forbidden access maps to Not Found", func(t *testing.T) {
		svc := &mockSetService{
			getCardsBySetIDFunc: func(_ context.Context, _ int64, _ uuid.UUID) ([]contracts.Card, error) {
				return nil, usecase.ErrForbidden
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetCardsBySetID(context.Background(), contracts.GetCardsBySetIDParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetCardsBySetIDNotFound); !ok {
			t.Fatalf("expected *contracts.GetCardsBySetIDNotFound, got %T", res)
		}
	})

	t.Run("Internal error returns 500", func(t *testing.T) {
		svc := &mockSetService{
			getCardsBySetIDFunc: func(_ context.Context, _ int64, _ uuid.UUID) ([]contracts.Card, error) {
				return nil, errors.New("db error")
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetCardsBySetID(context.Background(), contracts.GetCardsBySetIDParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetCardsBySetIDInternalServerError); !ok {
			t.Fatalf("expected *contracts.GetCardsBySetIDInternalServerError, got %T", res)
		}
	})
}

func TestSetsHandler_GetSetShareCode(t *testing.T) {
	setID := uuid.New()
	expectedSet := &contracts.CardSet{
		ID:    setID,
		Title: "Shared Set",
		Author: contracts.User{
			ID:        777,
			FirstName: "Author",
		},
		ShareCode: contracts.NewOptString("XYZ123"),
	}

	t.Run("Success returns CardSet with shareCode", func(t *testing.T) {
		svc := &mockSetService{
			getSetShareCodeFunc: func(_ context.Context, userID int64, id uuid.UUID) (*contracts.CardSet, error) {
				if userID != 777 {
					t.Fatalf("expected userID 777, got %d", userID)
				}
				if id != setID {
					t.Fatalf("expected setID %s, got %s", setID, id)
				}
				return expectedSet, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetShareCode(WithUserID(context.Background(), 777), contracts.GetSetShareCodeParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cs, ok := res.(*contracts.CardSet)
		if !ok {
			t.Fatalf("expected *contracts.CardSet, got %T", res)
		}
		if cs.ShareCode.Value != "XYZ123" {
			t.Errorf("expected share code XYZ123, got %s", cs.ShareCode.Value)
		}
	})

	t.Run("Not Found returns GetSetShareCodeNotFound", func(t *testing.T) {
		svc := &mockSetService{
			getSetShareCodeFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
				return nil, usecase.ErrNotFound
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetShareCode(context.Background(), contracts.GetSetShareCodeParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetShareCodeNotFound); !ok {
			t.Fatalf("expected *contracts.GetSetShareCodeNotFound, got %T", res)
		}
	})

	t.Run("Forbidden returns GetSetShareCodeNotFound for relational isolation", func(t *testing.T) {
		svc := &mockSetService{
			getSetShareCodeFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
				return nil, usecase.ErrForbidden
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetShareCode(context.Background(), contracts.GetSetShareCodeParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetShareCodeNotFound); !ok {
			t.Fatalf("expected *contracts.GetSetShareCodeNotFound, got %T", res)
		}
	})

	t.Run("Internal error returns GetSetShareCodeInternalServerError", func(t *testing.T) {
		svc := &mockSetService{
			getSetShareCodeFunc: func(_ context.Context, _ int64, _ uuid.UUID) (*contracts.CardSet, error) {
				return nil, errors.New("crypto failure")
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetShareCode(context.Background(), contracts.GetSetShareCodeParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetShareCodeInternalServerError); !ok {
			t.Fatalf("expected *contracts.GetSetShareCodeInternalServerError, got %T", res)
		}
	})
}

func TestSetsHandler_GetSetPlan(t *testing.T) {
	targetSetID := uuid.New()
	otherSetID := uuid.New()
	now := time.Now().UTC()

	planItems := []contracts.SetPlanItem{
		{FactName: "Fact A", Date: now},
		{FactName: "Fact B", Date: now.Add(24 * time.Hour)},
	}

	t.Run("Success returns SetPlanResponse", func(t *testing.T) {
		svc := &mockSetService{
			getSetPlanFunc: func(_ context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error) {
				if userID != 100 {
					t.Fatalf("expected userID 100, got %d", userID)
				}
				if setID != targetSetID {
					t.Fatalf("expected setID %s, got %s", targetSetID, setID)
				}
				return planItems, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetPlan(WithUserID(context.Background(), 100), contracts.GetSetPlanParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		planRes, ok := res.(*contracts.SetPlanResponse)
		if !ok {
			t.Fatalf("expected *contracts.SetPlanResponse, got %T", res)
		}
		if len(*planRes) != 2 || (*planRes)[0].FactName != "Fact A" {
			t.Errorf("unexpected plan items: %+v", *planRes)
		}
	})

	t.Run("Relational integrity: plan strictly scoped to set", func(t *testing.T) {
		svc := &mockSetService{
			getSetPlanFunc: func(_ context.Context, _ int64, setID uuid.UUID) ([]contracts.SetPlanItem, error) {
				if setID != targetSetID {
					return nil, usecase.ErrNotFound
				}
				return planItems, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetPlan(context.Background(), contracts.GetSetPlanParams{SetId: otherSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetPlanNotFound); !ok {
			t.Fatalf("expected *contracts.GetSetPlanNotFound, got %T", res)
		}
	})

	t.Run("Forbidden access maps to Not Found", func(t *testing.T) {
		svc := &mockSetService{
			getSetPlanFunc: func(_ context.Context, _ int64, _ uuid.UUID) ([]contracts.SetPlanItem, error) {
				return nil, usecase.ErrForbidden
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetPlan(context.Background(), contracts.GetSetPlanParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetPlanNotFound); !ok {
			t.Fatalf("expected *contracts.GetSetPlanNotFound, got %T", res)
		}
	})

	t.Run("Internal error returns 500", func(t *testing.T) {
		svc := &mockSetService{
			getSetPlanFunc: func(_ context.Context, _ int64, _ uuid.UUID) ([]contracts.SetPlanItem, error) {
				return nil, errors.New("query failed")
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.GetSetPlan(context.Background(), contracts.GetSetPlanParams{SetId: targetSetID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetSetPlanInternalServerError); !ok {
			t.Fatalf("expected *contracts.GetSetPlanInternalServerError, got %T", res)
		}
	})
}

func TestSetsHandler_JoinSetByShareCode(t *testing.T) {
	joinedSet := &contracts.CardSet{
		ID:         uuid.New(),
		Title:      "Joined Chemistry",
		CardsTotal: 15,
		CardsDue:   5,
		Author: contracts.User{
			ID:        555,
			FirstName: "Author",
		},
	}

	t.Run("Success joins set with code", func(t *testing.T) {
		svc := &mockSetService{
			joinSetByShareCodeFunc: func(_ context.Context, userID int64, code string) (*contracts.CardSet, error) {
				if userID != 555 {
					t.Fatalf("expected userID 555, got %d", userID)
				}
				if code != "ABCDEF" {
					t.Fatalf("expected code ABCDEF, got %s", code)
				}
				return joinedSet, nil
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.JoinSetByShareCode(
			WithUserID(context.Background(), 555),
			&contracts.JoinSetRequest{Code: "ABCDEF"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cs, ok := res.(*contracts.CardSet)
		if !ok {
			t.Fatalf("expected *contracts.CardSet, got %T", res)
		}
		if cs.Title != "Joined Chemistry" {
			t.Errorf("unexpected set title: %s", cs.Title)
		}
	})

	t.Run("Nil request body returns JoinSetByShareCodeBadRequest", func(t *testing.T) {
		h := NewSetsHandler(&mockSetService{})
		res, err := h.JoinSetByShareCode(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.JoinSetByShareCodeBadRequest); !ok {
			t.Fatalf("expected *contracts.JoinSetByShareCodeBadRequest, got %T", res)
		}
	})

	t.Run("Empty code returns JoinSetByShareCodeBadRequest", func(t *testing.T) {
		h := NewSetsHandler(&mockSetService{})
		res, err := h.JoinSetByShareCode(context.Background(), &contracts.JoinSetRequest{Code: "   "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.JoinSetByShareCodeBadRequest); !ok {
			t.Fatalf("expected *contracts.JoinSetByShareCodeBadRequest, got %T", res)
		}
	})

	t.Run("Validation error from service maps to JoinSetByShareCodeBadRequest", func(t *testing.T) {
		svc := &mockSetService{
			joinSetByShareCodeFunc: func(_ context.Context, _ int64, _ string) (*contracts.CardSet, error) {
				return nil, usecase.ErrValidation
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.JoinSetByShareCode(context.Background(), &contracts.JoinSetRequest{Code: "INVALID"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.JoinSetByShareCodeBadRequest); !ok {
			t.Fatalf("expected *contracts.JoinSetByShareCodeBadRequest, got %T", res)
		}
	})

	t.Run("Not Found error maps to JoinSetByShareCodeNotFound", func(t *testing.T) {
		svc := &mockSetService{
			joinSetByShareCodeFunc: func(_ context.Context, _ int64, _ string) (*contracts.CardSet, error) {
				return nil, usecase.ErrNotFound
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.JoinSetByShareCode(context.Background(), &contracts.JoinSetRequest{Code: "UNKNOWN"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.JoinSetByShareCodeNotFound); !ok {
			t.Fatalf("expected *contracts.JoinSetByShareCodeNotFound, got %T", res)
		}
	})

	t.Run("Internal error maps to JoinSetByShareCodeInternalServerError", func(t *testing.T) {
		svc := &mockSetService{
			joinSetByShareCodeFunc: func(_ context.Context, _ int64, _ string) (*contracts.CardSet, error) {
				return nil, errors.New("server crashed")
			},
		}
		h := NewSetsHandler(svc)
		res, err := h.JoinSetByShareCode(context.Background(), &contracts.JoinSetRequest{Code: "CRASH1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.JoinSetByShareCodeInternalServerError); !ok {
			t.Fatalf("expected *contracts.JoinSetByShareCodeInternalServerError, got %T", res)
		}
	})
}
