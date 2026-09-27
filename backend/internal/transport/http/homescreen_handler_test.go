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

type mockHomescreenService struct {
	getTodayFunc         func(ctx context.Context, userID int64) (*contracts.TodayData, error)
	getFeedQuestionsFunc func(ctx context.Context, userID int64) ([]contracts.Card, error)
	sendResultsFunc      func(ctx context.Context, userID int64, results []contracts.AnswerResult) error
}

func (m *mockHomescreenService) GetToday(ctx context.Context, userID int64) (*contracts.TodayData, error) {
	if m.getTodayFunc != nil {
		return m.getTodayFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockHomescreenService) GetFeedQuestions(ctx context.Context, userID int64) ([]contracts.Card, error) {
	if m.getFeedQuestionsFunc != nil {
		return m.getFeedQuestionsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockHomescreenService) SendResults(ctx context.Context, userID int64, results []contracts.AnswerResult) error {
	if m.sendResultsFunc != nil {
		return m.sendResultsFunc(ctx, userID, results)
	}
	return nil
}

func TestHomescreenHandler_GetToday(t *testing.T) {
	setID := uuid.New()
	expectedToday := &contracts.TodayData{
		UserName:         "Alex",
		ActiveDays:       4,
		DueCount:         15,
		EstimatedMinutes: 20,
		Sets: []contracts.CardSet{
			{
				ID:         setID,
				Title:      "Go Concurrency",
				CardsTotal: 30,
				CardsDue:   15,
			},
		},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		mockSetup    func(t *testing.T) *mockHomescreenService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.GetTodayRes)
	}{
		{
			name: "Success returns TodayData and propagates userID",
			ctx:  WithUserID(context.Background(), 12345),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						return expectedToday, nil
					},
				}
			},
			wantResType: (*contracts.TodayData)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetTodayRes) {
				today, ok := res.(*contracts.TodayData)
				if !ok {
					t.Fatalf("expected *contracts.TodayData, got %T", res)
				}
				if today.UserName != "Alex" {
					t.Fatalf("expected UserName 'Alex', got %q", today.UserName)
				}
				if today.ActiveDays != 4 {
					t.Fatalf("expected ActiveDays 4, got %d", today.ActiveDays)
				}
				if today.DueCount != 15 {
					t.Fatalf("expected DueCount 15, got %d", today.DueCount)
				}
				if len(today.Sets) != 1 || today.Sets[0].ID != setID {
					t.Fatalf("expected sets to contain %s, got %+v", setID, today.Sets)
				}
			},
		},
		{
			name: "Context without UserID falls back to 0",
			ctx:  context.Background(),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return expectedToday, nil
					},
				}
			},
			wantResType: (*contracts.TodayData)(nil),
		},
		{
			name: "Relational isolation strictly scopes data to requesting user",
			ctx:  WithUserID(context.Background(), 99999),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
						if userID != 99999 {
							t.Fatalf("expected userID 99999, got %d", userID)
						}
						// User 99999 has separate empty stats and no enrolled sets
						return &contracts.TodayData{
							UserName:         "NewUser",
							ActiveDays:       0,
							DueCount:         0,
							EstimatedMinutes: 0,
							Sets:             []contracts.CardSet{},
						}, nil
					},
				}
			},
			wantResType: (*contracts.TodayData)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetTodayRes) {
				today, ok := res.(*contracts.TodayData)
				if !ok {
					t.Fatalf("expected *contracts.TodayData, got %T", res)
				}
				if len(today.Sets) != 0 {
					t.Fatalf("expected 0 sets for isolated user, got %d", len(today.Sets))
				}
			},
		},
		{
			name: "Service error maps to ErrorResponse",
			ctx:  WithUserID(context.Background(), 12345),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						return nil, errors.New("database connection failed")
					},
				}
			},
			wantResType: (*contracts.ErrorResponse)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetTodayRes) {
				errResp, ok := res.(*contracts.ErrorResponse)
				if !ok {
					t.Fatalf("expected *contracts.ErrorResponse, got %T", res)
				}
				if errResp.Message != "database connection failed" {
					t.Fatalf("expected error message 'database connection failed', got %q", errResp.Message)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mockSetup(t)
			handler := NewHomescreenHandler(svc)

			res, err := handler.GetToday(tc.ctx)
			if err != nil {
				t.Fatalf("unexpected error from handler: %v", err)
			}

			switch tc.wantResType.(type) {
			case *contracts.TodayData:
				if _, ok := res.(*contracts.TodayData); !ok {
					t.Fatalf("expected *contracts.TodayData, got %T", res)
				}
			case *contracts.ErrorResponse:
				if _, ok := res.(*contracts.ErrorResponse); !ok {
					t.Fatalf("expected *contracts.ErrorResponse, got %T", res)
				}
			default:
				t.Fatalf("unhandled wantResType: %T", tc.wantResType)
			}

			if tc.checkResFunc != nil {
				tc.checkResFunc(t, res)
			}
		})
	}
}

func TestHomescreenHandler_GetFeedQuestions(t *testing.T) {
	card1 := contracts.Card{
		ID:       uuid.New(),
		SetId:    uuid.New(),
		Kind:     contracts.CardKindInput,
		Question: "What is a goroutine?",
		Topic:    "Go",
	}
	card2 := contracts.Card{
		ID:       uuid.New(),
		SetId:    uuid.New(),
		Kind:     contracts.CardKindChoice,
		Question: "Which channel operation blocks?",
		Options:  []string{"Send to unbuffered", "Read from buffered with items"},
		Topic:    "Go",
	}

	tests := []struct {
		name         string
		ctx          context.Context
		mockSetup    func(t *testing.T) *mockHomescreenService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.GetFeedQuestionsRes)
	}{
		{
			name: "Success returns feed cards and propagates userID",
			ctx:  WithUserID(context.Background(), 12345),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						return []contracts.Card{card1, card2}, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				feed, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON)
				if !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
				if len(*feed) != 2 {
					t.Fatalf("expected 2 cards, got %d", len(*feed))
				}
				if (*feed)[0].ID != card1.ID || (*feed)[1].ID != card2.ID {
					t.Fatalf("unexpected cards returned: %+v", *feed)
				}
			},
		},
		{
			name: "Empty feed returns empty slice and not nil",
			ctx:  WithUserID(context.Background(), 12345),
			mockSetup: func(_ *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, _ int64) ([]contracts.Card, error) {
						return nil, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				feed, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON)
				if !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
				if feed == nil || *feed == nil {
					t.Fatalf("expected non-nil slice pointer and non-nil slice, got %v", feed)
				}
				if len(*feed) != 0 {
					t.Fatalf("expected 0 cards, got %d", len(*feed))
				}
			},
		},
		{
			name: "Context without UserID falls back to 0",
			ctx:  context.Background(),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return []contracts.Card{}, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
		},
		{
			name: "Relational integrity ensures cards belong strictly to user active study queue",
			ctx:  WithUserID(context.Background(), 54321),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
						if userID != 54321 {
							t.Fatalf("expected userID 54321, got %d", userID)
						}
						// User 54321 has only card2 due
						return []contracts.Card{card2}, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				feed, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON)
				if !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
				if len(*feed) != 1 || (*feed)[0].ID != card2.ID {
					t.Fatalf("expected only card2 in user feed, got %+v", *feed)
				}
			},
		},
		{
			name: "Service error maps to ErrorResponse",
			ctx:  WithUserID(context.Background(), 12345),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						return nil, errors.New("failed to retrieve feed")
					},
				}
			},
			wantResType: (*contracts.ErrorResponse)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				errResp, ok := res.(*contracts.ErrorResponse)
				if !ok {
					t.Fatalf("expected *contracts.ErrorResponse, got %T", res)
				}
				if errResp.Message != "failed to retrieve feed" {
					t.Fatalf("expected 'failed to retrieve feed', got %q", errResp.Message)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mockSetup(t)
			handler := NewHomescreenHandler(svc)

			res, err := handler.GetFeedQuestions(tc.ctx)
			if err != nil {
				t.Fatalf("unexpected error from handler: %v", err)
			}

			switch tc.wantResType.(type) {
			case *contracts.GetFeedQuestionsOKApplicationJSON:
				if _, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON); !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
			case *contracts.ErrorResponse:
				if _, ok := res.(*contracts.ErrorResponse); !ok {
					t.Fatalf("expected *contracts.ErrorResponse, got %T", res)
				}
			default:
				t.Fatalf("unhandled wantResType: %T", tc.wantResType)
			}

			if tc.checkResFunc != nil {
				tc.checkResFunc(t, res)
			}
		})
	}
}

func TestHomescreenHandler_SendResults(t *testing.T) {
	cardID1 := uuid.New()
	cardID2 := uuid.New()
	now := time.Now()
	validResults := contracts.SendResultsRequest{
		contracts.AnswerResult{CardId: cardID1, Correct: true, AnsweredAt: now},
		contracts.AnswerResult{CardId: cardID2, Correct: false, AnsweredAt: now},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		req          contracts.SendResultsRequest
		mockSetup    func(t *testing.T) *mockHomescreenService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.SendResultsRes)
	}{
		{
			name: "Success submits results and returns SendResultsNoContent",
			ctx:  WithUserID(context.Background(), 12345),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if len(results) != 2 {
							t.Fatalf("expected 2 results, got %d", len(results))
						}
						if results[0].CardId != cardID1 || !results[0].Correct {
							t.Fatalf("unexpected first result: %+v", results[0])
						}
						if results[1].CardId != cardID2 || results[1].Correct {
							t.Fatalf("unexpected second result: %+v", results[1])
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.SendResultsNoContent)(nil),
		},
		{
			name: "Context without UserID falls back to 0",
			ctx:  context.Background(),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, _ []contracts.AnswerResult) error {
						if userID != 0 {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return nil
					},
				}
			},
			wantResType: (*contracts.SendResultsNoContent)(nil),
		},
		{
			name: "Nil results slice returns SendResultsBadRequest",
			ctx:  WithUserID(context.Background(), 12345),
			req:  nil,
			mockSetup: func(_ *testing.T) *mockHomescreenService {
				return &mockHomescreenService{}
			},
			wantResType: (*contracts.SendResultsBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				badReq, ok := res.(*contracts.SendResultsBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
				if badReq.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name: "Empty results slice returns SendResultsBadRequest",
			ctx:  WithUserID(context.Background(), 12345),
			req:  contracts.SendResultsRequest{},
			mockSetup: func(_ *testing.T) *mockHomescreenService {
				return &mockHomescreenService{}
			},
			wantResType: (*contracts.SendResultsBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				badReq, ok := res.(*contracts.SendResultsBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
				if badReq.Message == "" {
					t.Fatalf("expected non-empty error message")
				}
			},
		},
		{
			name: "Validation error from service maps to SendResultsBadRequest",
			ctx:  WithUserID(context.Background(), 12345),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if len(results) != len(validResults) {
							t.Fatalf("expected %d results, got %d", len(validResults), len(results))
						}
						return usecase.ErrValidation
					},
				}
			},
			wantResType: (*contracts.SendResultsBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				badReq, ok := res.(*contracts.SendResultsBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
				if badReq.Message != usecase.ErrValidation.Error() {
					t.Fatalf("expected %q, got %q", usecase.ErrValidation.Error(), badReq.Message)
				}
			},
		},
		{
			name: "Relational isolation error (ErrNotFound) maps to SendResultsBadRequest",
			ctx:  WithUserID(context.Background(), 12345),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if len(results) != len(validResults) {
							t.Fatalf("expected %d results, got %d", len(validResults), len(results))
						}
						return usecase.ErrNotFound
					},
				}
			},
			wantResType: (*contracts.SendResultsBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				badReq, ok := res.(*contracts.SendResultsBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
				if badReq.Message != usecase.ErrNotFound.Error() {
					t.Fatalf("expected %q, got %q", usecase.ErrNotFound.Error(), badReq.Message)
				}
			},
		},
		{
			name: "Relational isolation error (ErrForbidden) maps to SendResultsBadRequest",
			ctx:  WithUserID(context.Background(), 12345),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if len(results) != len(validResults) {
							t.Fatalf("expected %d results, got %d", len(validResults), len(results))
						}
						return usecase.ErrForbidden
					},
				}
			},
			wantResType: (*contracts.SendResultsBadRequest)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				badReq, ok := res.(*contracts.SendResultsBadRequest)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
				if badReq.Message != usecase.ErrForbidden.Error() {
					t.Fatalf("expected %q, got %q", usecase.ErrForbidden.Error(), badReq.Message)
				}
			},
		},
		{
			name: "Unexpected internal error maps to SendResultsInternalServerError",
			ctx:  WithUserID(context.Background(), 12345),
			req:  validResults,
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
						if userID != 12345 {
							t.Fatalf("expected userID 12345, got %d", userID)
						}
						if len(results) != len(validResults) {
							t.Fatalf("expected %d results, got %d", len(validResults), len(results))
						}
						return errors.New("disk write failure")
					},
				}
			},
			wantResType: (*contracts.SendResultsInternalServerError)(nil),
			checkResFunc: func(t *testing.T, res contracts.SendResultsRes) {
				errResp, ok := res.(*contracts.SendResultsInternalServerError)
				if !ok {
					t.Fatalf("expected *contracts.SendResultsInternalServerError, got %T", res)
				}
				if errResp.Message != "disk write failure" {
					t.Fatalf("expected 'disk write failure', got %q", errResp.Message)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mockSetup(t)
			handler := NewHomescreenHandler(svc)

			res, err := handler.SendResults(tc.ctx, tc.req)
			if err != nil {
				t.Fatalf("unexpected error from handler: %v", err)
			}

			switch tc.wantResType.(type) {
			case *contracts.SendResultsNoContent:
				if _, ok := res.(*contracts.SendResultsNoContent); !ok {
					t.Fatalf("expected *contracts.SendResultsNoContent, got %T", res)
				}
			case *contracts.SendResultsBadRequest:
				if _, ok := res.(*contracts.SendResultsBadRequest); !ok {
					t.Fatalf("expected *contracts.SendResultsBadRequest, got %T", res)
				}
			case *contracts.SendResultsInternalServerError:
				if _, ok := res.(*contracts.SendResultsInternalServerError); !ok {
					t.Fatalf("expected *contracts.SendResultsInternalServerError, got %T", res)
				}
			default:
				t.Fatalf("unhandled wantResType: %T", tc.wantResType)
			}

			if tc.checkResFunc != nil {
				tc.checkResFunc(t, res)
			}
		})
	}
}
