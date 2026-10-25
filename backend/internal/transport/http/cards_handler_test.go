package http

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

var testUserID = uuid.MustParse("00000000-0000-0000-0000-000000012345")

type mockCardService struct {
	usecase.CardService
	updateCardFunc     func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	deleteCardFunc     func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
	answerQuestionFunc func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error)
	revealCardFunc     func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (*contracts.CardReveal, error)
	reportIssueFunc    func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, reason contracts.CardIssueReason, comment string) error
	setIssuesFunc      func(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetIssue, error)
	resolveIssuesFunc  func(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
}

func (m *mockCardService) GetSetIssues(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetIssue, error) {
	if m.setIssuesFunc != nil {
		return m.setIssuesFunc(ctx, userID, setID)
	}
	return nil, nil
}

func (m *mockCardService) ResolveCardIssues(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error {
	if m.resolveIssuesFunc != nil {
		return m.resolveIssuesFunc(ctx, userID, cardID)
	}
	return nil
}

func (m *mockCardService) ReportCardIssue(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, reason contracts.CardIssueReason, comment string) error {
	if m.reportIssueFunc != nil {
		return m.reportIssueFunc(ctx, userID, cardID, reason, comment)
	}
	return nil
}

func (m *mockCardService) UpdateCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
	if m.updateCardFunc != nil {
		return m.updateCardFunc(ctx, userID, cardID, req)
	}
	return nil, nil
}

func (m *mockCardService) DeleteCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error {
	if m.deleteCardFunc != nil {
		return m.deleteCardFunc(ctx, userID, cardID)
	}
	return nil
}

func (m *mockCardService) AnswerQuestion(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, answer string, _ uuid.UUID) (*contracts.AnswerQuestionResponse, error) {
	if m.answerQuestionFunc != nil {
		return m.answerQuestionFunc(ctx, userID, cardID, answer)
	}
	return nil, nil
}

func (m *mockCardService) RevealCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (*contracts.CardReveal, error) {
	if m.revealCardFunc != nil {
		return m.revealCardFunc(ctx, userID, cardID)
	}
	return nil, nil
}

func TestCardsHandler_UpdateCard(t *testing.T) {
	cardID := uuid.New()
	setID := uuid.New()
	expectedCard := &contracts.Card{
		ID:          cardID,
		SetId:       setID,
		Kind:        contracts.CardKindChoice,
		Question:    "Updated question?",
		Options:     []string{"A", "B"},
		Explanation: contracts.NewOptString("Updated explanation"),
		FactId:      uuid.New(),
		FactName:    "General",
	}

	tests := []struct {
		name         string
		ctx          context.Context
		cardID       uuid.UUID
		req          *contracts.UpdateCardRequest
		mockSetup    func(t *testing.T) *mockCardService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.UpdateCardRes)
	}{
		{
			name:   "Success returns Card and propagates userID",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req: &contracts.UpdateCardRequest{
				Question: contracts.NewOptString("Updated question?"),
			},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if !req.Question.IsSet() || req.Question.Value != "Updated question?" {
							t.Fatalf("expected question 'Updated question?', got %v", req.Question)
						}
						return expectedCard, nil
					},
				}
			},
			wantResType: (*contracts.Card)(nil),
			checkResFunc: func(t *testing.T, res contracts.UpdateCardRes) {
				card, ok := res.(*contracts.Card)
				if !ok {
					t.Fatalf("expected *contracts.Card, got %T", res)
				}
				if card.ID != cardID {
					t.Fatalf("expected ID %s, got %s", cardID, card.ID)
				}
			},
		},
		{
			name:   "Context without UserID falls back to 0",
			ctx:    context.Background(),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, _ uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != uuid.Nil {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return expectedCard, nil
					},
				}
			},
			wantResType: (*contracts.Card)(nil),
		},
		{
			name:   "Nil request body returns UpdateCardBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    nil,
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.UpdateCardBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.UpdateCardRes) {
				badReq, ok := res.(*contracts.UpdateCardBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.UpdateCardBadRequest, got %T", res)
				}
				if badReq.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name:   "Validation error from service maps to UpdateCardBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return nil, usecase.ErrValidation
					},
				}
			},
			wantResType: (*contracts.UpdateCardBadRequest)(nil),
		},
		{
			name:   "Not Found error maps to UpdateCardNotFound",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return nil, usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.UpdateCardNotFound)(nil),
		},
		{
			name:   "Forbidden error maps to UpdateCardNotFound to prevent leaking resource existence",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return nil, usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.UpdateCardNotFound)(nil),
		},
		{
			name:   "Internal error maps to UpdateCardInternalServerError",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return nil, errors.New("db error")
					},
				}
			},
			wantResType: (*contracts.UpdateCardInternalServerError)(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.mockSetup(t)
			h := NewCardsHandler(svc)

			res, err := h.UpdateCard(tt.ctx, tt.req, contracts.UpdateCardParams{CardId: tt.cardID})
			if err != nil {
				t.Fatalf("unexpected error returned by handler: %v", err)
			}

			switch tt.wantResType.(type) {
			case *contracts.Card:
				if _, ok := res.(*contracts.Card); !ok {
					t.Fatalf("expected *contracts.Card, got %T", res)
				}
			case *contracts.UpdateCardBadRequest:
				if _, ok := res.(*contracts.UpdateCardBadRequest); !ok {
					t.Fatalf("expected *contracts.UpdateCardBadRequest, got %T", res)
				}
			case *contracts.UpdateCardNotFound:
				if _, ok := res.(*contracts.UpdateCardNotFound); !ok {
					t.Fatalf("expected *contracts.UpdateCardNotFound, got %T", res)
				}
			case *contracts.UpdateCardInternalServerError:
				if _, ok := res.(*contracts.UpdateCardInternalServerError); !ok {
					t.Fatalf("expected *contracts.UpdateCardInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unknown wantResType: %T", tt.wantResType)
			}

			if tt.checkResFunc != nil {
				tt.checkResFunc(t, res)
			}
		})
	}
}

func TestCardsHandler_UpdateCard_RelationalIntegrity(t *testing.T) {
	userOwnedCardID := uuid.New()
	foreignCardID := uuid.New()

	service := &mockCardService{
		updateCardFunc: func(_ context.Context, userID uuid.UUID, cardID uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
			if userID != testUserID {
				t.Errorf("expected userID 100, got %d", userID)
			}
			if cardID != userOwnedCardID {
				// Assert: Never allow modifying cards belonging to other sets or users
				return nil, usecase.ErrForbidden
			}
			return &contracts.Card{ID: userOwnedCardID}, nil
		},
	}

	h := NewCardsHandler(service)

	// User requests updating their own card -> success
	res, err := h.UpdateCard(WithUserID(context.Background(), testUserID), &contracts.UpdateCardRequest{}, contracts.UpdateCardParams{CardId: userOwnedCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*contracts.Card); !ok {
		t.Fatalf("expected 200 Card, got %T", res)
	}

	// User requests updating someone else's card -> 404 NotFound
	resForeign, err := h.UpdateCard(WithUserID(context.Background(), testUserID), &contracts.UpdateCardRequest{}, contracts.UpdateCardParams{CardId: foreignCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resForeign.(*contracts.UpdateCardNotFound); !ok {
		t.Fatalf("expected 404 UpdateCardNotFound, got %T", resForeign)
	}
}

func TestCardsHandler_DeleteCard(t *testing.T) {
	cardID := uuid.New()

	tests := []struct {
		name         string
		ctx          context.Context
		cardID       uuid.UUID
		mockSetup    func(t *testing.T) *mockCardService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.DeleteCardRes)
	}{
		{
			name:   "Success returns DeleteCardNoContent",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) error {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.DeleteCardNoContent)(nil),
		},
		{
			name:   "Context without UserID falls back to 0",
			ctx:    context.Background(),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID uuid.UUID, _ uuid.UUID) error {
						if userID != uuid.Nil {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.DeleteCardNoContent)(nil),
		},
		{
			name:   "Not Found returns DeleteCardNotFound",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) error {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.DeleteCardNotFound)(nil),
			checkResFunc: func(t *testing.T, res contracts.DeleteCardRes) {
				notFound, ok := res.(*contracts.DeleteCardNotFound)
				if !ok {
					t.Fatalf("expected *contracts.DeleteCardNotFound, got %T", res)
				}
				if notFound.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name:   "Forbidden returns DeleteCardNotFound to prevent information leakage",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) error {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.DeleteCardNotFound)(nil),
		},
		{
			name:   "Internal error returns DeleteCardInternalServerError",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) error {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return errors.New("delete failed")
					},
				}
			},
			wantResType: (*contracts.DeleteCardInternalServerError)(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.mockSetup(t)
			h := NewCardsHandler(svc)

			res, err := h.DeleteCard(tt.ctx, contracts.DeleteCardParams{CardId: tt.cardID})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			switch tt.wantResType.(type) {
			case *contracts.DeleteCardNoContent:
				if _, ok := res.(*contracts.DeleteCardNoContent); !ok {
					t.Fatalf("expected *contracts.DeleteCardNoContent, got %T", res)
				}
			case *contracts.DeleteCardNotFound:
				if _, ok := res.(*contracts.DeleteCardNotFound); !ok {
					t.Fatalf("expected *contracts.DeleteCardNotFound, got %T", res)
				}
			case *contracts.DeleteCardInternalServerError:
				if _, ok := res.(*contracts.DeleteCardInternalServerError); !ok {
					t.Fatalf("expected *contracts.DeleteCardInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unknown wantResType: %T", tt.wantResType)
			}

			if tt.checkResFunc != nil {
				tt.checkResFunc(t, res)
			}
		})
	}
}

func TestCardsHandler_DeleteCard_RelationalIntegrity(t *testing.T) {
	userOwnedCardID := uuid.New()
	foreignCardID := uuid.New()

	service := &mockCardService{
		deleteCardFunc: func(_ context.Context, userID uuid.UUID, cardID uuid.UUID) error {
			if userID != testUserID {
				t.Errorf("expected userID 100, got %d", userID)
			}
			if cardID != userOwnedCardID {
				// Assert: Never allow deleting cards belonging to other sets/users
				return usecase.ErrForbidden
			}
			return nil
		},
	}

	h := NewCardsHandler(service)

	// User deletes their own card -> 204 NoContent
	res, err := h.DeleteCard(WithUserID(context.Background(), testUserID), contracts.DeleteCardParams{CardId: userOwnedCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*contracts.DeleteCardNoContent); !ok {
		t.Fatalf("expected 204 DeleteCardNoContent, got %T", res)
	}

	// User attempts to delete foreign card -> 404 NotFound
	resForeign, err := h.DeleteCard(WithUserID(context.Background(), testUserID), contracts.DeleteCardParams{CardId: foreignCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resForeign.(*contracts.DeleteCardNotFound); !ok {
		t.Fatalf("expected 404 DeleteCardNotFound, got %T", resForeign)
	}
}

func TestCardsHandler_RevealCard(t *testing.T) {
	flipID, inputID, answeredID := uuid.New(), uuid.New(), uuid.New()
	ctx := WithUserID(context.Background(), testUserID)
	svc := &mockCardService{
		revealCardFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) (*contracts.CardReveal, error) {
			if userID != testUserID {
				t.Fatalf("service got user %s", userID)
			}
			switch id {
			case flipID:
				return &contracts.CardReveal{Answer: contracts.NewStringCardAnswer("отсортированный")}, nil
			case inputID:
				return nil, usecase.ErrValidation
			case answeredID:
				return nil, usecase.ErrConflict
			}
			return nil, usecase.ErrNotFound
		},
	}
	h := NewCardsHandler(svc)

	if res, err := h.RevealCard(ctx, contracts.RevealCardParams{CardId: flipID}); err != nil {
		t.Fatal(err)
	} else if got, ok := res.(*contracts.CardReveal); !ok || got.Answer.String != "отсортированный" {
		t.Errorf("flip: res %#v; want the service's reveal", res)
	}
	if res, _ := h.RevealCard(ctx, contracts.RevealCardParams{CardId: inputID}); res == nil {
		t.Error("input: no response")
	} else if _, ok := res.(*contracts.RevealCardBadRequest); !ok {
		t.Errorf("input: res %T, want bad request", res)
	}
	if res, _ := h.RevealCard(ctx, contracts.RevealCardParams{CardId: answeredID}); res == nil {
		t.Error("out of the feed: no response")
	} else if _, ok := res.(*contracts.RevealCardConflict); !ok {
		t.Errorf("out of the feed: res %T, want conflict", res)
	}
	if res, _ := h.RevealCard(ctx, contracts.RevealCardParams{CardId: uuid.New()}); res == nil {
		t.Error("unknown card: no response")
	} else if _, ok := res.(*contracts.RevealCardNotFound); !ok {
		t.Errorf("unknown card: res %T, want not found", res)
	}
}

func TestCardsHandler_AnswerQuestion(t *testing.T) {
	cardID := uuid.New()

	tests := []struct {
		name         string
		ctx          context.Context
		cardID       uuid.UUID
		req          *contracts.AnswerQuestionRequest
		mockSetup    func(t *testing.T) *mockCardService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.AnswerQuestionRes)
	}{
		{
			name:   "Success correct answer returns AnswerQuestionResponse with IsCorrect true",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "42"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if answer != "42" {
							t.Fatalf("expected answer '42', got %s", answer)
						}
						return &contracts.AnswerQuestionResponse{
							IsCorrect: true,
							Method:    contracts.AnswerQuestionResponseMethodLocal,
						}, nil
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionResponse)(nil),
			checkResFunc: func(t *testing.T, res contracts.AnswerQuestionRes) {
				ans, ok := res.(*contracts.AnswerQuestionResponse)
				if !ok {
					t.Fatalf("expected *contracts.AnswerQuestionResponse, got %T", res)
				}
				if !ans.IsCorrect {
					t.Fatalf("expected correct answer 42, got %+v", ans)
				}
			},
		},
		{
			name:   "Success incorrect answer returns AnswerQuestionResponse with IsCorrect false",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "wrong"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, _ string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return &contracts.AnswerQuestionResponse{
							IsCorrect: false,
							Method:    contracts.AnswerQuestionResponseMethodLocal,
						}, nil
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionResponse)(nil),
			checkResFunc: func(t *testing.T, res contracts.AnswerQuestionRes) {
				ans, ok := res.(*contracts.AnswerQuestionResponse)
				if !ok {
					t.Fatalf("expected *contracts.AnswerQuestionResponse, got %T", res)
				}
				if ans.IsCorrect {
					t.Fatalf("expected incorrect answer 'wrong', got %+v", ans)
				}
			},
		},
		{
			name:   "Context without UserID falls back to 0",
			ctx:    context.Background(),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "42"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, _ uuid.UUID, _ string) (*contracts.AnswerQuestionResponse, error) {
						if userID != uuid.Nil {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return &contracts.AnswerQuestionResponse{IsCorrect: true, Method: contracts.AnswerQuestionResponseMethodLocal}, nil
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionResponse)(nil),
		},
		{
			name:   "Nil request body returns AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    nil,
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.AnswerQuestionRes) {
				badReq, ok := res.(*contracts.AnswerQuestionBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.AnswerQuestionBadRequest, got %T", res)
				}
				if badReq.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name:   "Empty answer returns AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: ""},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
		},
		{
			name:   "Whitespace-only answer returns AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "   "},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
		},
		{
			name:   "Validation error from service maps to AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if ans != "foo" {
							t.Fatalf("expected answer 'foo', got %s", ans)
						}
						return nil, usecase.ErrValidation
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
		},
		{
			name:   "A card out of the feed maps to AnswerQuestionConflict",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(context.Context, uuid.UUID, uuid.UUID, string) (*contracts.AnswerQuestionResponse, error) {
						return nil, fmt.Errorf("%w: the card is not in the feed", usecase.ErrConflict)
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionConflict)(nil),
		},
		{
			name:   "Not Found error maps to AnswerQuestionNotFound",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if ans != "foo" {
							t.Fatalf("expected answer 'foo', got %s", ans)
						}
						return nil, usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionNotFound)(nil),
		},
		{
			name:   "Forbidden error maps to AnswerQuestionNotFound for relational isolation",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if ans != "foo" {
							t.Fatalf("expected answer 'foo', got %s", ans)
						}
						return nil, usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionNotFound)(nil),
		},
		{
			name:   "Internal error maps to AnswerQuestionInternalServerError",
			ctx:    WithUserID(context.Background(), testUserID),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != testUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if ans != "foo" {
							t.Fatalf("expected answer 'foo', got %s", ans)
						}
						return nil, errors.New("internal failure")
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionInternalServerError)(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.mockSetup(t)
			h := NewCardsHandler(svc)

			res, err := h.AnswerQuestion(tt.ctx, tt.req, contracts.AnswerQuestionParams{CardId: tt.cardID})
			if err != nil {
				t.Fatalf("unexpected error returned by handler: %v", err)
			}

			switch tt.wantResType.(type) {
			case *contracts.AnswerQuestionResponse:
				if _, ok := res.(*contracts.AnswerQuestionResponse); !ok {
					t.Fatalf("expected *contracts.AnswerQuestionResponse, got %T", res)
				}
			case *contracts.AnswerQuestionBadRequest:
				if _, ok := res.(*contracts.AnswerQuestionBadRequest); !ok {
					t.Fatalf("expected *contracts.AnswerQuestionBadRequest, got %T", res)
				}
			case *contracts.AnswerQuestionNotFound:
				if _, ok := res.(*contracts.AnswerQuestionNotFound); !ok {
					t.Fatalf("expected *contracts.AnswerQuestionNotFound, got %T", res)
				}
			case *contracts.AnswerQuestionConflict:
				if _, ok := res.(*contracts.AnswerQuestionConflict); !ok {
					t.Fatalf("expected *contracts.AnswerQuestionConflict, got %T", res)
				}
			case *contracts.AnswerQuestionInternalServerError:
				if _, ok := res.(*contracts.AnswerQuestionInternalServerError); !ok {
					t.Fatalf("expected *contracts.AnswerQuestionInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unknown wantResType: %T", tt.wantResType)
			}

			if tt.checkResFunc != nil {
				tt.checkResFunc(t, res)
			}
		})
	}
}

func TestCardsHandler_ReportCardIssue(t *testing.T) {
	cardID := uuid.New()
	testUserID := uuid.New()
	tests := []struct {
		name    string
		req     *contracts.ReportCardIssueRequest
		err     error
		want    any
		reached bool
	}{
		{"saved", &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonNotInNotes}, nil, (*contracts.ReportCardIssueNoContent)(nil), true},
		{"saved with a comment", &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther, Comment: contracts.NewOptString("Опечатка")}, nil, (*contracts.ReportCardIssueNoContent)(nil), true},
		{"other without a comment", &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther}, usecase.ErrValidation, (*contracts.ReportCardIssueBadRequest)(nil), true},
		{"no body", nil, nil, (*contracts.ReportCardIssueBadRequest)(nil), false},
		{"unknown reason", &contracts.ReportCardIssueRequest{Reason: "bad"}, nil, (*contracts.ReportCardIssueBadRequest)(nil), false},
		{"card not visible", &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonAnswer}, usecase.ErrNotFound, (*contracts.ReportCardIssueNotFound)(nil), true},
		{"database down", &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonAnswer}, errors.New("connection reset"), (*contracts.ReportCardIssueInternalServerError)(nil), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			h := NewCardsHandler(&mockCardService{
				reportIssueFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID, reason contracts.CardIssueReason, comment string) error {
					reached = true
					if userID != testUserID || id != cardID || reason != tt.req.Reason || comment != tt.req.Comment.Value {
						t.Fatalf("got user %s, card %s, reason %q, comment %q", userID, id, reason, comment)
					}
					return tt.err
				},
			})
			res, err := h.ReportCardIssue(WithUserID(context.Background(), testUserID), tt.req, contracts.ReportCardIssueParams{CardId: cardID})
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(res) != reflect.TypeOf(tt.want) {
				t.Errorf("res = %T, want %T", res, tt.want)
			}
			if reached != tt.reached {
				t.Errorf("service reached = %v, want %v", reached, tt.reached)
			}
		})
	}
}

func TestCardsHandler_GetSetIssues(t *testing.T) {
	setID := uuid.New()
	testUserID := uuid.New()
	issues := []contracts.SetIssue{{CardId: uuid.New(), Reason: contracts.CardIssueReasonAnswer, Count: 2}}
	tests := []struct {
		name   string
		issues []contracts.SetIssue
		err    error
		want   any
	}{
		{"author", issues, nil, (*contracts.GetSetIssuesOKApplicationJSON)(nil)},
		{"no open complaints", nil, nil, (*contracts.GetSetIssuesOKApplicationJSON)(nil)},
		{"not the author", nil, usecase.ErrNotFound, (*contracts.GetSetIssuesNotFound)(nil)},
		{"database down", nil, errors.New("connection reset"), (*contracts.GetSetIssuesInternalServerError)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewCardsHandler(&mockCardService{
				setIssuesFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) ([]contracts.SetIssue, error) {
					if userID != testUserID || id != setID {
						t.Fatalf("got user %s, set %s", userID, id)
					}
					return tt.issues, tt.err
				},
			})
			res, err := h.GetSetIssues(WithUserID(context.Background(), testUserID), contracts.GetSetIssuesParams{SetId: setID})
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(res) != reflect.TypeOf(tt.want) {
				t.Fatalf("res = %T, want %T", res, tt.want)
			}
			// An empty list must go out as [], not null.
			if list, ok := res.(*contracts.GetSetIssuesOKApplicationJSON); ok && (*list == nil || len(*list) != len(tt.issues)) {
				t.Errorf("list = %#v, want %d complaints", *list, len(tt.issues))
			}
		})
	}
}

func TestCardsHandler_ResolveCardIssues(t *testing.T) {
	cardID := uuid.New()
	testUserID := uuid.New()
	tests := []struct {
		name string
		err  error
		want any
	}{
		{"author", nil, (*contracts.ResolveCardIssuesNoContent)(nil)},
		{"not the author", usecase.ErrNotFound, (*contracts.ResolveCardIssuesNotFound)(nil)},
		{"database down", errors.New("connection reset"), (*contracts.ResolveCardIssuesInternalServerError)(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewCardsHandler(&mockCardService{
				resolveIssuesFunc: func(_ context.Context, userID uuid.UUID, id uuid.UUID) error {
					if userID != testUserID || id != cardID {
						t.Fatalf("got user %s, card %s", userID, id)
					}
					return tt.err
				},
			})
			res, err := h.ResolveCardIssues(WithUserID(context.Background(), testUserID), contracts.ResolveCardIssuesParams{CardId: cardID})
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(res) != reflect.TypeOf(tt.want) {
				t.Errorf("res = %T, want %T", res, tt.want)
			}
		})
	}
}
