package http

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockCardService struct {
	updateCardFunc      func(ctx context.Context, userID int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	deleteCardFunc      func(ctx context.Context, userID int64, cardID uuid.UUID) error
	answerQuestionFunc  func(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error)
	checkAnswerFunc     func(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.CheckAnswerResponse, error)
	reportCardIssueFunc func(ctx context.Context, userID int64, cardID uuid.UUID, reason contracts.CardIssueReason) error
}

func (m *mockCardService) UpdateCard(ctx context.Context, userID int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
	if m.updateCardFunc != nil {
		return m.updateCardFunc(ctx, userID, cardID, req)
	}
	return nil, nil
}

func (m *mockCardService) DeleteCard(ctx context.Context, userID int64, cardID uuid.UUID) error {
	if m.deleteCardFunc != nil {
		return m.deleteCardFunc(ctx, userID, cardID)
	}
	return nil
}

func (m *mockCardService) AnswerQuestion(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
	if m.answerQuestionFunc != nil {
		return m.answerQuestionFunc(ctx, userID, cardID, answer)
	}
	return nil, nil
}

func (m *mockCardService) CheckAnswer(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.CheckAnswerResponse, error) {
	if m.checkAnswerFunc != nil {
		return m.checkAnswerFunc(ctx, userID, cardID, answer)
	}
	return nil, nil
}

func (m *mockCardService) ReportCardIssue(ctx context.Context, userID int64, cardID uuid.UUID, reason contracts.CardIssueReason) error {
	if m.reportCardIssueFunc != nil {
		return m.reportCardIssueFunc(ctx, userID, cardID, reason)
	}
	return nil
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
		Explanation: "Updated explanation",
		Topic:       "General",
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req: &contracts.UpdateCardRequest{
				Question: contracts.NewOptString("Updated question?"),
			},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
					updateCardFunc: func(_ context.Context, userID int64, _ uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 0 {
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
			ctx:    WithUserID(context.Background(), 12345),
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.UpdateCardRequest{},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
		updateCardFunc: func(_ context.Context, userID int64, cardID uuid.UUID, _ *contracts.UpdateCardRequest) (*contracts.Card, error) {
			if userID != 100 {
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
	res, err := h.UpdateCard(WithUserID(context.Background(), 100), &contracts.UpdateCardRequest{}, contracts.UpdateCardParams{CardId: userOwnedCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*contracts.Card); !ok {
		t.Fatalf("expected 200 Card, got %T", res)
	}

	// User requests updating someone else's card -> 404 NotFound
	resForeign, err := h.UpdateCard(WithUserID(context.Background(), 100), &contracts.UpdateCardRequest{}, contracts.UpdateCardParams{CardId: foreignCardID})
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
					deleteCardFunc: func(_ context.Context, userID int64, _ uuid.UUID) error {
						if userID != 0 {
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					deleteCardFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
		deleteCardFunc: func(_ context.Context, userID int64, cardID uuid.UUID) error {
			if userID != 100 {
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
	res, err := h.DeleteCard(WithUserID(context.Background(), 100), contracts.DeleteCardParams{CardId: userOwnedCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*contracts.DeleteCardNoContent); !ok {
		t.Fatalf("expected 204 DeleteCardNoContent, got %T", res)
	}

	// User attempts to delete foreign card -> 404 NotFound
	resForeign, err := h.DeleteCard(WithUserID(context.Background(), 100), contracts.DeleteCardParams{CardId: foreignCardID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resForeign.(*contracts.DeleteCardNotFound); !ok {
		t.Fatalf("expected 404 DeleteCardNotFound, got %T", resForeign)
	}
}

func TestCardsHandler_CheckAnswer(t *testing.T) {
	cardID := uuid.New()
	ctx := WithUserID(context.Background(), 12345)
	svc := &mockCardService{
		checkAnswerFunc: func(_ context.Context, userID int64, id uuid.UUID, answer string) (*contracts.CheckAnswerResponse, error) {
			if id != cardID {
				return nil, usecase.ErrNotFound
			}
			if userID != 12345 || answer != "массив упорядочен" {
				t.Fatalf("service got user %d, answer %q", userID, answer)
			}
			return &contracts.CheckAnswerResponse{IsCorrect: true, Method: contracts.CheckAnswerResponseMethodModel}, nil
		},
	}
	h := NewCardsHandler(svc)

	res, err := h.CheckAnswer(ctx, &contracts.CheckAnswerRequest{Answer: "  массив упорядочен "}, contracts.CheckAnswerParams{CardId: cardID})
	if got, ok := res.(*contracts.CheckAnswerResponse); err != nil || !ok || !got.IsCorrect {
		t.Errorf("answer: res %#v, err %v; want the service's verdict", res, err)
	}
	if res, _ := h.CheckAnswer(ctx, &contracts.CheckAnswerRequest{Answer: "  "}, contracts.CheckAnswerParams{CardId: cardID}); res == nil {
		t.Error("empty answer: no response")
	} else if _, ok := res.(*contracts.CheckAnswerBadRequest); !ok {
		t.Errorf("empty answer: res %T, want bad request", res)
	}
	if res, _ := h.CheckAnswer(ctx, &contracts.CheckAnswerRequest{Answer: "массив упорядочен"}, contracts.CheckAnswerParams{CardId: uuid.New()}); res == nil {
		t.Error("unknown card: no response")
	} else if _, ok := res.(*contracts.CheckAnswerNotFound); !ok {
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "42"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if answer != "42" {
							t.Fatalf("expected answer '42', got %s", answer)
						}
						return &contracts.AnswerQuestionResponse{
							IsCorrect:  true,
							UserAnswer: "42",
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
				if !ans.IsCorrect || ans.UserAnswer != "42" {
					t.Fatalf("expected correct answer 42, got %+v", ans)
				}
			},
		},
		{
			name:   "Success incorrect answer returns AnswerQuestionResponse with IsCorrect false",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "wrong"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						return &contracts.AnswerQuestionResponse{
							IsCorrect:  false,
							UserAnswer: answer,
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
				if ans.IsCorrect || ans.UserAnswer != "wrong" {
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
					answerQuestionFunc: func(_ context.Context, userID int64, _ uuid.UUID, _ string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return &contracts.AnswerQuestionResponse{IsCorrect: true, UserAnswer: "42"}, nil
					},
				}
			},
			wantResType: (*contracts.AnswerQuestionResponse)(nil),
		},
		{
			name:   "Nil request body returns AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: ""},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
		},
		{
			name:   "Whitespace-only answer returns AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "   "},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.AnswerQuestionBadRequest)(nil),
		},
		{
			name:   "Validation error from service maps to AnswerQuestionBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			name:   "Not Found error maps to AnswerQuestionNotFound",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.AnswerQuestionRequest{Answer: "foo"},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, ans string) (*contracts.AnswerQuestionResponse, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
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

	tests := []struct {
		name         string
		ctx          context.Context
		cardID       uuid.UUID
		req          *contracts.ReportCardIssueRequest
		mockSetup    func(t *testing.T) *mockCardService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.ReportCardIssueRes)
	}{
		{
			name:   "Success returns ReportCardIssueNoContent",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonAnswer},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if reason != contracts.CardIssueReasonAnswer {
							t.Fatalf("expected reason 'answer', got %s", reason)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueNoContent)(nil),
		},
		{
			name:   "Context without UserID falls back to 0",
			ctx:    context.Background(),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonWording},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, _ uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						if reason != contracts.CardIssueReasonWording {
							t.Fatalf("expected reason 'wording', got %s", reason)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueNoContent)(nil),
		},
		{
			name:   "Nil request body returns ReportCardIssueBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    nil,
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.ReportCardIssueBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.ReportCardIssueRes) {
				badReq, ok := res.(*contracts.ReportCardIssueBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.ReportCardIssueBadRequest, got %T", res)
				}
				if badReq.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name:   "Empty reason returns ReportCardIssueBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: ""},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.ReportCardIssueBadRequest)(nil),
		},
		{
			name:   "Whitespace reason returns ReportCardIssueBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: "   "},
			mockSetup: func(_ *testing.T) *mockCardService {
				return &mockCardService{}
			},
			wantResType: (*contracts.ReportCardIssueBadRequest)(nil),
		},
		{
			name:   "Validation error from service maps to ReportCardIssueBadRequest",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if reason != contracts.CardIssueReasonOther {
							t.Fatalf("expected reason 'other', got %s", reason)
						}
						return usecase.ErrValidation
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueBadRequest)(nil),
		},
		{
			name:   "Not Found error maps to ReportCardIssueNotFound",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonNotInNotes},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if reason != contracts.CardIssueReasonNotInNotes {
							t.Fatalf("expected reason 'not-in-notes', got %s", reason)
						}
						return usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueNotFound)(nil),
		},
		{
			name:   "Forbidden error maps to ReportCardIssueNotFound for relational isolation",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if reason != contracts.CardIssueReasonOther {
							t.Fatalf("expected reason 'other', got %s", reason)
						}
						return usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueNotFound)(nil),
		},
		{
			name:   "Internal error maps to ReportCardIssueInternalServerError",
			ctx:    WithUserID(context.Background(), 12345),
			cardID: cardID,
			req:    &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonAnswer},
			mockSetup: func(t *testing.T) *mockCardService {
				return &mockCardService{
					reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if id != cardID {
							t.Fatalf("expected cardID %s, got %s", cardID, id)
						}
						if reason != contracts.CardIssueReasonAnswer {
							t.Fatalf("expected reason 'answer', got %s", reason)
						}
						return errors.New("report failed")
					},
				}
			},
			wantResType: (*contracts.ReportCardIssueInternalServerError)(nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.mockSetup(t)
			h := NewCardsHandler(svc)

			res, err := h.ReportCardIssue(tt.ctx, tt.req, contracts.ReportCardIssueParams{CardId: tt.cardID})
			if err != nil {
				t.Fatalf("unexpected error returned by handler: %v", err)
			}

			switch tt.wantResType.(type) {
			case *contracts.ReportCardIssueNoContent:
				if _, ok := res.(*contracts.ReportCardIssueNoContent); !ok {
					t.Fatalf("expected *contracts.ReportCardIssueNoContent, got %T", res)
				}
			case *contracts.ReportCardIssueBadRequest:
				if _, ok := res.(*contracts.ReportCardIssueBadRequest); !ok {
					t.Fatalf("expected *contracts.ReportCardIssueBadRequest, got %T", res)
				}
			case *contracts.ReportCardIssueNotFound:
				if _, ok := res.(*contracts.ReportCardIssueNotFound); !ok {
					t.Fatalf("expected *contracts.ReportCardIssueNotFound, got %T", res)
				}
			case *contracts.ReportCardIssueInternalServerError:
				if _, ok := res.(*contracts.ReportCardIssueInternalServerError); !ok {
					t.Fatalf("expected *contracts.ReportCardIssueInternalServerError, got %T", res)
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
