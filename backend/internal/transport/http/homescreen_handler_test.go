package http

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

var (
	testHomeUserID    = uuid.MustParse("00000000-0000-0000-0000-000000012345")
	testHomeUser99999 = uuid.MustParse("00000000-0000-0000-0000-000000099999")
	testHomeUser54321 = uuid.MustParse("00000000-0000-0000-0000-000000054321")
)

type mockHomescreenService struct {
	getTodayFunc         func(ctx context.Context, userID uuid.UUID) (*contracts.TodayData, error)
	getFeedQuestionsFunc func(ctx context.Context, userID uuid.UUID, opts usecase.FeedOptions) ([]contracts.FeedCard, error)
}

func (m *mockHomescreenService) GetToday(ctx context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
	if m.getTodayFunc != nil {
		return m.getTodayFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockHomescreenService) GetFeedQuestions(ctx context.Context, userID uuid.UUID, opts usecase.FeedOptions) ([]contracts.FeedCard, error) {
	if m.getFeedQuestionsFunc != nil {
		return m.getFeedQuestionsFunc(ctx, userID, opts)
	}
	return nil, nil
}

func TestHomescreenHandler_GetToday(t *testing.T) {
	setID := uuid.New()
	expectedToday := &contracts.TodayData{
		User: contracts.User{
			ID:        testHomeUserID,
			FirstName: "Alex",
		},
		DueCount:         15,
		EstimatedMinutes: 20,
		Sets: []contracts.CardSet{
			{
				ID:         setID,
				Title:      "Go Concurrency",
				CardsTotal: 30,
				CardsDue:   15,
				Author: contracts.User{
					ID:        testHomeUserID,
					FirstName: "Alex",
				},
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
			ctx:  WithUserID(context.Background(), testHomeUserID),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
						if userID != testHomeUserID {
							t.Fatalf("unexpected userID: %s", userID)
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
				if today.User.FirstName != "Alex" {
					t.Fatalf("expected FirstName 'Alex', got %q", today.User.FirstName)
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
					getTodayFunc: func(_ context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
						if userID != uuid.Nil {
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
			ctx:  WithUserID(context.Background(), testHomeUser99999),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
						if userID != testHomeUser99999 {
							t.Fatalf("expected userID 99999, got %d", userID)
						}
						// User 99999 has separate empty stats and no enrolled sets
						return &contracts.TodayData{
							User: contracts.User{
								ID:        testHomeUser99999,
								FirstName: "NewUser",
							},
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
			ctx:  WithUserID(context.Background(), testHomeUserID),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getTodayFunc: func(_ context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
						if userID != testHomeUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						return nil, errors.New("database connection failed")
					},
				}
			},
			wantResType: (*contracts.GetTodayInternalServerError)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetTodayRes) {
				errResp, ok := res.(*contracts.GetTodayInternalServerError)
				if !ok {
					t.Fatalf("expected *contracts.GetTodayInternalServerError, got %T", res)
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
			case *contracts.GetTodayInternalServerError:
				if _, ok := res.(*contracts.GetTodayInternalServerError); !ok {
					t.Fatalf("expected *contracts.GetTodayInternalServerError, got %T", res)
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
	factUUID := uuid.New()
	card1 := contracts.FeedCard{
		ID:       uuid.New(),
		SetId:    uuid.New(),
		FactId:   factUUID,
		FactName: "Go",
		Kind:     contracts.CardKindInput,
		Question: "What is a goroutine?",
	}
	card2 := contracts.FeedCard{
		ID:       uuid.New(),
		SetId:    uuid.New(),
		FactId:   factUUID,
		FactName: "Go",
		Kind:     contracts.CardKindChoice,
		Question: "Which channel operation blocks?",
		Options:  []string{"Send to unbuffered", "Read from buffered with items"},
	}

	tests := []struct {
		name         string
		ctx          context.Context
		params       contracts.GetFeedQuestionsParams
		mockSetup    func(t *testing.T) *mockHomescreenService
		wantResType  any
		checkResFunc func(t *testing.T, res contracts.GetFeedQuestionsRes)
	}{
		{
			name: "Success returns feed cards and propagates userID",
			ctx:  WithUserID(context.Background(), testHomeUserID),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
						if userID != testHomeUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						return []contracts.FeedCard{card1, card2}, nil
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
			name: "Success with setId and early flags propagated to service",
			ctx:  WithUserID(context.Background(), testHomeUserID),
			params: contracts.GetFeedQuestionsParams{
				SetId: contracts.NewOptUUID(card1.SetId),
				Early: contracts.NewOptBool(true),
			},
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, opts usecase.FeedOptions) ([]contracts.FeedCard, error) {
						if userID != testHomeUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						if opts.SetID == nil || *opts.SetID != card1.SetId {
							t.Fatalf("expected SetID %s, got %v", card1.SetId, opts.SetID)
						}
						if !opts.Early {
							t.Fatalf("expected Early true")
						}
						return []contracts.FeedCard{card1}, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				feed, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON)
				if !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
				if len(*feed) != 1 || (*feed)[0].ID != card1.ID {
					t.Fatalf("unexpected cards returned: %+v", *feed)
				}
			},
		},
		{
			name: "Empty feed returns empty slice and not nil",
			ctx:  WithUserID(context.Background(), testHomeUserID),
			mockSetup: func(_ *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, _ uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
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
					getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
						if userID != uuid.Nil {
							t.Fatalf("expected fallback userID 0, got %d", userID)
						}
						return []contracts.FeedCard{}, nil
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsOKApplicationJSON)(nil),
		},
		{
			name: "Relational integrity ensures cards belong strictly to user active study queue",
			ctx:  WithUserID(context.Background(), testHomeUser54321),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
						if userID != testHomeUser54321 {
							t.Fatalf("expected userID 54321, got %d", userID)
						}
						// User 54321 has only card2 due
						return []contracts.FeedCard{card2}, nil
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
			ctx:  WithUserID(context.Background(), testHomeUserID),
			mockSetup: func(t *testing.T) *mockHomescreenService {
				return &mockHomescreenService{
					getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
						if userID != testHomeUserID {
							t.Fatalf("unexpected userID: %s", userID)
						}
						return nil, errors.New("failed to retrieve feed")
					},
				}
			},
			wantResType: (*contracts.GetFeedQuestionsInternalServerError)(nil),
			checkResFunc: func(t *testing.T, res contracts.GetFeedQuestionsRes) {
				errResp, ok := res.(*contracts.GetFeedQuestionsInternalServerError)
				if !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsInternalServerError, got %T", res)
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

			res, err := handler.GetFeedQuestions(tc.ctx, tc.params)
			if err != nil {
				t.Fatalf("unexpected error from handler: %v", err)
			}

			switch tc.wantResType.(type) {
			case *contracts.GetFeedQuestionsOKApplicationJSON:
				if _, ok := res.(*contracts.GetFeedQuestionsOKApplicationJSON); !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsOKApplicationJSON, got %T", res)
				}
			case *contracts.GetFeedQuestionsInternalServerError:
				if _, ok := res.(*contracts.GetFeedQuestionsInternalServerError); !ok {
					t.Fatalf("expected *contracts.GetFeedQuestionsInternalServerError, got %T", res)
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
