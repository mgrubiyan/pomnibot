package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	ht "github.com/ogen-go/ogen/http"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

var _ contracts.Handler = (*APIHandler)(nil)

func TestAPIHandler_GetHealth(t *testing.T) {
	h := NewAPIHandler(nil, nil, nil)
	res, err := h.GetHealth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, ok := res.(*contracts.HealthResponse)
	if !ok {
		t.Fatalf("expected *contracts.HealthResponse, got %T", res)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
}

func TestAPIHandler_SetsDelegation(t *testing.T) {
	setID := uuid.New()
	mockSet := &mockSetService{
		getSetFunc: func(_ context.Context, userID int64, id uuid.UUID) (*contracts.CardSet, error) {
			if userID != 123 || id != setID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return &contracts.CardSet{ID: id, Title: "Math"}, nil
		},
		deleteSetFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
			if userID != 123 || id != setID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return nil
		},
		getCardsBySetIDFunc: func(_ context.Context, userID int64, id uuid.UUID) ([]contracts.Card, error) {
			if userID != 123 || id != setID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return []contracts.Card{{ID: uuid.New(), SetId: id, Kind: contracts.CardKindChoice, Question: "Q1"}}, nil
		},
		getSetShareCodeFunc: func(_ context.Context, userID int64, id uuid.UUID) (*contracts.CardSet, error) {
			if userID != 123 || id != setID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return &contracts.CardSet{ID: id, ShareCode: contracts.NewOptString("CODE123")}, nil
		},
		getSetPlanFunc: func(_ context.Context, userID int64, id uuid.UUID) ([]contracts.SetPlanItem, error) {
			if userID != 123 || id != setID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return []contracts.SetPlanItem{{FactName: "Fact 1", Date: time.Now()}}, nil
		},
		joinSetByShareCodeFunc: func(_ context.Context, userID int64, code string) (*contracts.CardSet, error) {
			if userID != 123 || code != "JOIN123" {
				t.Errorf("unexpected args: userID=%d, code=%s", userID, code)
			}
			return &contracts.CardSet{ID: setID, Title: "Joined"}, nil
		},
	}

	h := NewAPIHandler(mockSet, nil, nil)
	ctx := WithUserID(context.Background(), 123)

	t.Run("GetSet delegates to setsHandler", func(t *testing.T) {
		res, err := h.GetSet(ctx, contracts.GetSetParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.CardSet); !ok {
			t.Fatalf("expected *contracts.CardSet, got %T", res)
		}
	})

	t.Run("DeleteSet delegates to setsHandler", func(t *testing.T) {
		res, err := h.DeleteSet(ctx, contracts.DeleteSetParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.DeleteSetNoContent); !ok {
			t.Fatalf("expected *contracts.DeleteSetNoContent, got %T", res)
		}
	})

	t.Run("GetCardsBySetID delegates to setsHandler", func(t *testing.T) {
		res, err := h.GetCardsBySetID(ctx, contracts.GetCardsBySetIDParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetCardsBySetIDOKApplicationJSON); !ok {
			t.Fatalf("expected *contracts.GetCardsBySetIDOKApplicationJSON, got %T", res)
		}
	})

	t.Run("GetSetShareCode delegates to setsHandler", func(t *testing.T) {
		res, err := h.GetSetShareCode(ctx, contracts.GetSetShareCodeParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.CardSet); !ok {
			t.Fatalf("expected *contracts.CardSet, got %T", res)
		}
	})

	t.Run("GetSetPlan delegates to setsHandler", func(t *testing.T) {
		res, err := h.GetSetPlan(ctx, contracts.GetSetPlanParams{SetId: setID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.SetPlanResponse); !ok {
			t.Fatalf("expected *contracts.SetPlanResponse, got %T", res)
		}
	})

	t.Run("JoinSetByShareCode delegates to setsHandler", func(t *testing.T) {
		res, err := h.JoinSetByShareCode(ctx, &contracts.JoinSetRequest{Code: "JOIN123"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.CardSet); !ok {
			t.Fatalf("expected *contracts.CardSet, got %T", res)
		}
	})
}

func TestAPIHandler_CardsDelegation(t *testing.T) {
	cardID := uuid.New()
	mockCard := &mockCardService{
		updateCardFunc: func(_ context.Context, userID int64, id uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
			if userID != 456 || id != cardID || req == nil {
				t.Errorf("unexpected args: userID=%d, id=%s, req=%v", userID, id, req)
			}
			return &contracts.Card{ID: id, SetId: uuid.New(), Kind: contracts.CardKindChoice, Question: "Updated Question"}, nil
		},
		deleteCardFunc: func(_ context.Context, userID int64, id uuid.UUID) error {
			if userID != 456 || id != cardID {
				t.Errorf("unexpected args: userID=%d, id=%s", userID, id)
			}
			return nil
		},
		answerQuestionFunc: func(_ context.Context, userID int64, id uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
			if userID != 456 || id != cardID || answer != "my answer" {
				t.Errorf("unexpected args: userID=%d, id=%s, answer=%s", userID, id, answer)
			}
			return &contracts.AnswerQuestionResponse{IsCorrect: true, UserAnswer: answer}, nil
		},
		reportCardIssueFunc: func(_ context.Context, userID int64, id uuid.UUID, reason contracts.CardIssueReason) error {
			if userID != 456 || id != cardID || reason != contracts.CardIssueReasonOther {
				t.Errorf("unexpected args: userID=%d, id=%s, reason=%s", userID, id, reason)
			}
			return nil
		},
	}

	h := NewAPIHandler(nil, mockCard, nil)
	ctx := WithUserID(context.Background(), 456)

	t.Run("UpdateCard delegates to cardsHandler", func(t *testing.T) {
		res, err := h.UpdateCard(ctx, &contracts.UpdateCardRequest{Question: contracts.NewOptString("Updated Question")}, contracts.UpdateCardParams{CardId: cardID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.Card); !ok {
			t.Fatalf("expected *contracts.Card, got %T", res)
		}
	})

	t.Run("DeleteCard delegates to cardsHandler", func(t *testing.T) {
		res, err := h.DeleteCard(ctx, contracts.DeleteCardParams{CardId: cardID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.DeleteCardNoContent); !ok {
			t.Fatalf("expected *contracts.DeleteCardNoContent, got %T", res)
		}
	})

	t.Run("AnswerQuestion delegates to cardsHandler", func(t *testing.T) {
		res, err := h.AnswerQuestion(ctx, &contracts.AnswerQuestionRequest{Answer: "my answer"}, contracts.AnswerQuestionParams{CardId: cardID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.AnswerQuestionResponse); !ok {
			t.Fatalf("expected *contracts.AnswerQuestionResponse, got %T", res)
		}
	})

	t.Run("ReportCardIssue delegates to cardsHandler", func(t *testing.T) {
		res, err := h.ReportCardIssue(ctx, &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther}, contracts.ReportCardIssueParams{CardId: cardID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.ReportCardIssueNoContent); !ok {
			t.Fatalf("expected *contracts.ReportCardIssueNoContent, got %T", res)
		}
	})
}

func TestAPIHandler_HomescreenDelegation(t *testing.T) {
	mockHome := &mockHomescreenService{
		getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
			if userID != 789 {
				t.Errorf("unexpected userID: %d", userID)
			}
			return &contracts.TodayData{UserName: "Alex", ActiveDays: 5, DueCount: 10, EstimatedMinutes: 15}, nil
		},
		getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
			if userID != 789 {
				t.Errorf("unexpected userID: %d", userID)
			}
			return []contracts.Card{{ID: uuid.New(), SetId: uuid.New(), Kind: contracts.CardKindChoice, Question: "Feed Card"}}, nil
		},
		sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
			if userID != 789 || len(results) != 1 {
				t.Errorf("unexpected args: userID=%d, len=%d", userID, len(results))
			}
			return nil
		},
	}

	h := NewAPIHandler(nil, nil, mockHome)
	ctx := WithUserID(context.Background(), 789)

	t.Run("GetToday delegates to homescreenHandler", func(t *testing.T) {
		res, err := h.GetToday(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.TodayData); !ok {
			t.Fatalf("expected *contracts.TodayData, got %T", res)
		}
	})

	t.Run("GetFeedQuestions delegates to homescreenHandler", func(t *testing.T) {
		res, err := h.GetFeedQuestions(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON); !ok {
			t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
		}
	})

	t.Run("SendResults delegates to homescreenHandler", func(t *testing.T) {
		res, err := h.SendResults(ctx, contracts.SendResultsRequest{
			contracts.AnswerResult{CardId: uuid.New(), Correct: true, AnsweredAt: time.Now()},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.SendResultsNoContent); !ok {
			t.Fatalf("expected *contracts.SendResultsNoContent, got %T", res)
		}
	})
}

func TestAPIHandler_NilServicesFallback(t *testing.T) {
	h := NewAPIHandler(nil, nil, nil)
	ctx := context.Background()
	randomID := uuid.New()

	t.Run("Health check always works even with nil services", func(t *testing.T) {
		res, err := h.GetHealth(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := res.(*contracts.HealthResponse); !ok {
			t.Fatalf("expected *contracts.HealthResponse, got %T", res)
		}
	})

	t.Run("Sets operations return ErrNotImplemented when sets service is nil", func(t *testing.T) {
		_, err := h.GetSet(ctx, contracts.GetSetParams{SetId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.DeleteSet(ctx, contracts.DeleteSetParams{SetId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.GetCardsBySetID(ctx, contracts.GetCardsBySetIDParams{SetId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.GetSetShareCode(ctx, contracts.GetSetShareCodeParams{SetId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.GetSetPlan(ctx, contracts.GetSetPlanParams{SetId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.JoinSetByShareCode(ctx, &contracts.JoinSetRequest{Code: "XYZ"})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}
	})

	t.Run("Cards operations return ErrNotImplemented when cards service is nil", func(t *testing.T) {
		_, err := h.UpdateCard(ctx, &contracts.UpdateCardRequest{}, contracts.UpdateCardParams{CardId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.DeleteCard(ctx, contracts.DeleteCardParams{CardId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.AnswerQuestion(ctx, &contracts.AnswerQuestionRequest{Answer: "test"}, contracts.AnswerQuestionParams{CardId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.ReportCardIssue(ctx, &contracts.ReportCardIssueRequest{Reason: contracts.CardIssueReasonOther}, contracts.ReportCardIssueParams{CardId: randomID})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}
	})

	t.Run("Homescreen operations return ErrNotImplemented when homescreen service is nil", func(t *testing.T) {
		_, err := h.GetToday(ctx)
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.GetFeedQuestions(ctx)
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}

		_, err = h.SendResults(ctx, contracts.SendResultsRequest{})
		if !errors.Is(err, ht.ErrNotImplemented) {
			t.Fatalf("expected ErrNotImplemented, got %v", err)
		}
	})
}
