package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestHealthEndpoint(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>Test</body></html>")},
	}

	router, err := NewRouter(NewAPIHandler(nil, nil, nil), mockFS)
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

	router, err := NewRouter(NewAPIHandler(nil, nil, nil), mockFS)
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
	capturedUserID int64
	capturedOk     bool
}

func (h *authTestHandler) GetHealth(ctx context.Context) (contracts.GetHealthRes, error) {
	h.capturedUserID, h.capturedOk = UserIDFromContext(ctx)
	return &contracts.HealthResponse{Status: "ok"}, nil
}

func TestRouter_AuthMiddlewareIntegration(t *testing.T) {
	t.Run("propagates valid user from X-Init-Data header", func(t *testing.T) {
		h := &authTestHandler{}
		router, err := NewRouter(h, fstest.MapFS{})
		if err != nil {
			t.Fatalf("failed to create router: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Header.Set("X-Init-Data", `user={"id":424242}`)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !h.capturedOk {
			t.Fatal("expected UserID to be present in context")
		}
		if h.capturedUserID != 424242 {
			t.Fatalf("expected userID 424242, got %d", h.capturedUserID)
		}
	})

	t.Run("falls back to 0 when X-Init-Data is absent", func(t *testing.T) {
		h := &authTestHandler{}
		router, err := NewRouter(h, fstest.MapFS{})
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
		if h.capturedUserID != 0 {
			t.Fatalf("expected fallback userID 0, got %d", h.capturedUserID)
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

	testUserID := int64(98765)
	testSetID := uuid.New()
	testCardID := uuid.New()

	mockSet := &mockSetService{
		getSetFunc: func(_ context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error) {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
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
		getCardsBySetIDFunc: func(_ context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error) {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
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
					Answer:   contracts.NewStringCardAnswer("4"),
				},
			}, nil
		},
	}

	mockCard := &mockCardService{
		answerQuestionFunc: func(_ context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
			}
			if cardID != testCardID {
				return nil, usecase.ErrNotFound
			}
			return &contracts.AnswerQuestionResponse{
				IsCorrect:  answer == "4",
				UserAnswer: answer,
			}, nil
		},
	}

	mockHome := &mockHomescreenService{
		getTodayFunc: func(_ context.Context, userID int64) (*contracts.TodayData, error) {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
			}
			return &contracts.TodayData{
				User: contracts.User{
					ID:        testUserID,
					FirstName: "Tester",
				},
				ActiveDays: 3,
				DueCount:   7,
			}, nil
		},
		getFeedQuestionsFunc: func(_ context.Context, userID int64) ([]contracts.Card, error) {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
			}
			return []contracts.Card{
				{
					ID:       testCardID,
					SetId:    testSetID,
					Kind:     contracts.CardKindChoice,
					Question: "Feed Question",
					Answer:   contracts.NewStringCardAnswer("Feed Answer"),
				},
			}, nil
		},
		sendResultsFunc: func(_ context.Context, userID int64, results []contracts.AnswerResult) error {
			if userID != testUserID {
				t.Errorf("expected userID %d, got %d", testUserID, userID)
			}
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
			return nil
		},
	}

	handler := NewAPIHandler(mockSet, mockCard, mockHome)
	router, err := NewRouter(handler, mockFS)
	if err != nil {
		t.Fatalf("failed to create router: %v", err)
	}

	authHeader := fmt.Sprintf(`user={"id":%d}`, testUserID)

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
		req.Header.Set("X-Init-Data", authHeader)
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

	t.Run("GET /api/sets/{setId}/cards with auth returns 200 and cards", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+testSetID.String()+"/cards", nil)
		req.Header.Set("X-Init-Data", authHeader)
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
		body := bytes.NewBufferString(`{"answer":"4"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/cards/"+testCardID.String()+"/answer", body)
		req.Header.Set("X-Init-Data", authHeader)
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
		if !resp.IsCorrect || resp.UserAnswer != "4" {
			t.Errorf("unexpected answer response: %+v", resp)
		}
	})

	t.Run("GET /api/ (today) with auth returns 200 TodayData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/", nil)
		req.Header.Set("X-Init-Data", authHeader)
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
		req.Header.Set("X-Init-Data", authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp []contracts.Card
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}
		if len(resp) != 1 || resp[0].Question != "Feed Question" {
			t.Errorf("unexpected feed response: %+v", resp)
		}
	})

	t.Run("POST /api/results with auth returns 204 No Content", func(t *testing.T) {
		body := bytes.NewBufferString(fmt.Sprintf(`[{"cardId":"%s","correct":true,"answeredAt":"2026-09-27T05:00:00Z"}]`, testCardID))
		req := httptest.NewRequest(http.MethodPost, "/api/results", body)
		req.Header.Set("X-Init-Data", authHeader)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Relational isolation returns 404 for unknown or cross-tenant set", func(t *testing.T) {
		unknownSetID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/api/sets/"+unknownSetID.String(), nil)
		req.Header.Set("X-Init-Data", authHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
