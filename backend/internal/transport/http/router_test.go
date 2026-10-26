package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestHealthEndpoint(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>Test</body></html>")},
	}

	router, err := NewRouter(NewAPIHandler(nil, nil, nil, nil), mockFS, "", "", nil, nil)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response contracts.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if response.Status != "ok" {
		t.Fatalf("expected status %q, got %q", "ok", response.Status)
	}
}

func TestStaticAndReactRouter(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><body>Test App</body></html>"),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte("console.log('hello');"),
		},
	}

	router, err := NewRouter(NewAPIHandler(nil, nil, nil, nil), mockFS, "", "", nil, nil)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
		expectedHeader string
		expectedAllow  string
	}{
		{
			name:           "Root path serves index.html",
			method:         http.MethodGet,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "Existing asset serves asset",
			method:         http.MethodGet,
			path:           "/assets/app.js",
			expectedStatus: http.StatusOK,
			expectedBody:   "console.log('hello');",
			expectedHeader: "public, max-age=31536000, immutable",
		},
		{
			name:           "React router path serves index.html",
			method:         http.MethodGet,
			path:           "/feed/card/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "React router HEAD serves index headers",
			method:         http.MethodHead,
			path:           "/feed/card/123",
			expectedStatus: http.StatusOK,
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "Unknown API path returns 404 for GET",
			method:         http.MethodGet,
			path:           "/api/cards",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Unknown API path returns 404 for POST",
			method:         http.MethodPost,
			path:           "/api/cards",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Missing asset returns 404",
			method:         http.MethodGet,
			path:           "/assets/missing.js",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Non-GET/HEAD request on missing route returns 405",
			method:         http.MethodPost,
			path:           "/feed/card/123",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedAllow:  "GET, HEAD",
		},
		{
			name:           "DELETE request on unknown path returns 405",
			method:         http.MethodDelete,
			path:           "/whatever",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedAllow:  "GET, HEAD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("path %s: expected status %d, got %d", tc.path, tc.expectedStatus, rec.Code)
			}

			if tc.expectedBody != "" && rec.Body.String() != tc.expectedBody {
				t.Errorf("path %s: expected body %q, got %q", tc.path, tc.expectedBody, rec.Body.String())
			}

			if tc.expectedHeader != "" {
				if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != tc.expectedHeader {
					t.Errorf("path %s: expected Cache-Control %q, got %q", tc.path, tc.expectedHeader, cacheControl)
				}
			}

			if tc.expectedAllow != "" {
				if allow := rec.Header().Get("Allow"); allow != tc.expectedAllow {
					t.Errorf("path %s: expected Allow header %q, got %q", tc.path, tc.expectedAllow, allow)
				}
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})

	wrapped := LoggingMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/custom-path", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rec.Code)
	}

	if rec.Body.String() != "short and stout" {
		t.Fatalf("expected body %q, got %q", "short and stout", rec.Body.String())
	}
}

type authTestHandler struct {
	contracts.UnimplementedHandler
	capturedUserID uuid.UUID
	capturedOk     bool
}

func (h *authTestHandler) GetHealth(ctx context.Context) (contracts.GetHealthRes, error) {
	h.capturedUserID, h.capturedOk = UserIDFromContext(ctx)
	return &contracts.HealthResponse{Status: "ok"}, nil
}

func TestRouter_AuthMiddlewareIntegration(t *testing.T) {
	testBotToken := "secret-bot-token"

	t.Run("propagates internal user ID from MAX authorization", func(t *testing.T) {
		h := &authTestHandler{}
		testAuthID := uuid.MustParse("00000000-0000-0000-0000-000000524242")
		mockUserSvc := &mockUserService{
			maxFunc: func(_ context.Context, p usecase.MAXUserParams) (usecase.Account, error) {
				return usecase.Account{
					User:            contracts.User{ID: testAuthID, FirstName: p.FirstName},
					MaxID:           p.MaxID,
					ProfileComplete: true,
				}, nil
			},
		}
		router, err := NewRouter(h, fstest.MapFS{}, testBotToken, "", mockUserSvc, nil)
		if err != nil {
			t.Fatalf("failed to create router: %v", err)
		}

		now := time.Now().Unix()
		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":424242,"first_name":"AuthTest"}`,
		}, testBotToken)

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("Authorization", "max "+initDataStr)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !h.capturedOk {
			t.Fatal("expected UserID to be present in context")
		}
		if h.capturedUserID != testAuthID {
			t.Fatalf("expected internal userID %s, got %s", testAuthID, h.capturedUserID)
		}
	})

	t.Run("falls back to 0 when authorization is absent", func(t *testing.T) {
		h := &authTestHandler{}
		router, err := NewRouter(h, fstest.MapFS{}, testBotToken, "", nil, nil)
		if err != nil {
			t.Fatalf("failed to create router: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !h.capturedOk {
			t.Fatal("expected UserID to be present in context")
		}
		if h.capturedUserID != uuid.Nil {
			t.Fatalf("expected fallback userID %s, got %s", uuid.Nil, h.capturedUserID)
		}
	})
}

func BenchmarkSPAHandler_Asset(b *testing.B) {
	mockFS := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>Test App</body></html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('hello');")},
	}
	handler := NewSPAHandler(mockFS)
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func TestRouter_E2E_Integration(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>Test</body></html>")},
	}

	testMaxID := int64(98765)
	testUserID := uuid.MustParse("00000000-0000-0000-0000-000000198765")
	testSetID := uuid.New()
	testCardID := uuid.New()

	mockSet := &mockSetService{
		getSetFunc: func(_ context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			if setID != testSetID {
				return nil, usecase.ErrNotFound
			}
			return &contracts.CardSet{
				ID:    setID,
				Title: "E2E Sets",
				Author: contracts.User{
					ID:        testUserID,
					FirstName: "Tester",
				},
			}, nil
		},
		getCardsBySetIDFunc: func(_ context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.Card, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			if setID != testSetID {
				return nil, usecase.ErrNotFound
			}
			return []contracts.Card{
				{
					ID:       testCardID,
					SetId:    setID,
					Kind:     contracts.CardKindChoice,
					Question: "What is 2+2?",
					Options:  []string{"3", "4", "5"},
					Answer:   contracts.NewOptCardAnswer(contracts.NewStringCardAnswer("4")),
				},
			}, nil
		},
		getSetLeaderboardFunc: func(_ context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetLeaderboardResponse, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			if setID != testSetID {
				return nil, usecase.ErrNotFound
			}
			return &contracts.SetLeaderboardResponse{
				SetId: setID,
				Items: []contracts.LeaderboardEntry{
					{
						User: contracts.User{
							ID:        testUserID,
							FirstName: "Tester",
						},
						Rank:       1,
						Percentile: 95,
					},
				},
			}, nil
		},
	}

	mockCard := &mockCardService{
		answerQuestionFunc: func(_ context.Context, userID uuid.UUID, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			if cardID != testCardID {
				return nil, usecase.ErrNotFound
			}
			return &contracts.AnswerQuestionResponse{
				IsCorrect: answer == "4",
				Method:    contracts.AnswerQuestionResponseMethodLocal,
			}, nil
		},
	}

	mockHome := &mockHomescreenService{
		getTodayFunc: func(_ context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			return &contracts.TodayData{
				User: contracts.User{
					ID:        testUserID,
					FirstName: "Tester",
				},
				DueCount: 7,
			}, nil
		},
		getFeedQuestionsFunc: func(_ context.Context, userID uuid.UUID, _ usecase.FeedOptions) ([]contracts.FeedCard, error) {
			if userID != testUserID {
				t.Errorf("expected userID %s, got %s", testUserID, userID)
			}
			return []contracts.FeedCard{
				{
					ID:       testCardID,
					SetId:    testSetID,
					Kind:     contracts.CardKindChoice,
					Question: "Feed Question",
					Options:  []string{"4", "5"},
				},
			}, nil
		},
	}

	testBotToken := "e2e-token"
	mockUserSvc := &mockUserService{
		maxFunc: func(_ context.Context, p usecase.MAXUserParams) (usecase.Account, error) {
			return usecase.Account{
				User: contracts.User{
					ID:        testUserID,
					FirstName: "Tester",
				},
				MaxID:           p.MaxID,
				ProfileComplete: true,
			}, nil
		},
	}
	handler := NewAPIHandler(mockSet, mockCard, mockHome, nil)
	router, err := NewRouter(handler, mockFS, testBotToken, "", mockUserSvc, nil)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}

	now := time.Now().Unix()
	authHeader := generateValidInitData(map[string]string{
		"auth_date": strconv.FormatInt(now, 10),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":"Tester"}`, testMaxID),
	}, testBotToken)

	t.Run("GET /api/health returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp contracts.HealthResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if resp.Status != "ok" {
			t.Errorf("expected status 'ok', got %q", resp.Status)
		}
	})

	t.Run("GET /api/sets/{setId} with auth returns 200 CardSet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+testSetID.String(), nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp contracts.CardSet
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if resp.ID != testSetID || resp.Title != "E2E Sets" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("GET /api/sets/{setId}/leaderboard with auth returns 200 SetLeaderboardResponse", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+testSetID.String()+"/leaderboard", nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp contracts.SetLeaderboardResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if resp.SetId != testSetID || len(resp.Items) != 1 || resp.Items[0].Percentile != 95 {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("GET /api/sets/{setId}/cards with auth returns 200 and cards", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+testSetID.String()+"/cards", nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp []contracts.Card
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if len(resp) != 1 || resp[0].ID != testCardID {
			t.Errorf("unexpected cards: %+v", resp)
		}
	})

	t.Run("POST /api/cards/{cardId}/answer with auth returns 200", func(t *testing.T) {
		body := bytes.NewBufferString(`{"answer":"4","attemptId":"01900000-0000-7000-8000-000000000001"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/cards/"+testCardID.String()+"/answer", body)
		req.Header.Set("Authorization", "max "+authHeader)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp contracts.AnswerQuestionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if !resp.IsCorrect || resp.Method != contracts.AnswerQuestionResponseMethodLocal {
			t.Errorf("unexpected answer response: %+v", resp)
		}
	})

	t.Run("GET /api/ (today) with auth returns 200 TodayData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/", nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp contracts.TodayData
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if resp.User.FirstName != "Tester" || resp.DueCount != 7 {
			t.Errorf("unexpected today data: %+v", resp)
		}
	})

	t.Run("GET /api/feed with auth returns 200 feed cards", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if len(resp) != 1 || resp[0]["question"] != "Feed Question" {
			t.Errorf("unexpected feed response: %+v", resp)
		}
		for _, hidden := range []string{"answer", "explanation", "sourceQuote", "sourceRef"} {
			if _, ok := resp[0][hidden]; ok {
				t.Errorf("the feed gives away %q: %s", hidden, rec.Body.String())
			}
		}
	})

	t.Run("GET /api/feed?setId=...&early=true with auth returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/feed?setId=%s&early=true", testSetID), nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if len(resp) != 1 || resp[0]["question"] != "Feed Question" {
			t.Errorf("unexpected feed response: %+v", resp)
		}
	})

	t.Run("POST /api/results is gone: the client cannot report its own verdict", func(t *testing.T) {
		body := bytes.NewBufferString(fmt.Sprintf(`[{"cardId":"%s","correct":true}]`, testCardID))
		req := httptest.NewRequest(http.MethodPost, "/api/results", body)
		req.Header.Set("Authorization", "max "+authHeader)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Relational isolation returns 404 for unknown or cross-tenant set", func(t *testing.T) {
		unknownSetID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+unknownSetID.String(), nil)
		req.Header.Set("Authorization", "max "+authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
