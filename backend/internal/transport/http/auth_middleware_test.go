package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockUserService struct {
	upsertUserFunc func(ctx context.Context, params usecase.UpsertUserParams) error
	ensureUserFunc func(ctx context.Context, userID int64) error
	upsertCalls    []usecase.UpsertUserParams
}

func (m *mockUserService) UpsertUser(ctx context.Context, params usecase.UpsertUserParams) error {
	m.upsertCalls = append(m.upsertCalls, params)
	if m.upsertUserFunc != nil {
		return m.upsertUserFunc(ctx, params)
	}
	return nil
}

func (m *mockUserService) EnsureUser(ctx context.Context, userID int64) error {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, userID)
	}
	return nil
}

func generateValidInitData(params map[string]string, botToken string) string {
	var keys []string
	for k := range params {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, params[k]))
	}
	launchParams := strings.Join(lines, "\n")

	h := hmac.New(sha256.New, []byte("WebAppData"))
	h.Write([]byte(botToken))
	secretKey := h.Sum(nil)

	h2 := hmac.New(sha256.New, secretKey)
	h2.Write([]byte(launchParams))
	hashVal := hex.EncodeToString(h2.Sum(nil))

	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, v)
	}
	vals.Set("hash", hashVal)
	return vals.Encode()
}

func TestAuthMiddleware(t *testing.T) {
	testBotToken := "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

	t.Run("Valid signed X-Init-Data extracts user and calls UpsertUser", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":12345,"first_name":"Max","last_name":"User","username":"maxuser","is_bot":false}`,
		}, testBotToken)

		var recordedUserID int64
		var recordedUserName string
		handlerCalled := false

		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			recordedUserID, _ = UserIDFromContext(r.Context())
			recordedUserName, _ = UserNameFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		if !handlerCalled {
			t.Fatal("expected downstream handler to be called")
		}
		if recordedUserID != 12345 {
			t.Fatalf("expected userID 12345, got %d", recordedUserID)
		}
		if recordedUserName != "Max User" {
			t.Fatalf("expected userName 'Max User', got %q", recordedUserName)
		}

		if len(mockUserSvc.upsertCalls) != 1 {
			t.Fatalf("expected 1 UpsertUser call, got %d", len(mockUserSvc.upsertCalls))
		}
		call := mockUserSvc.upsertCalls[0]
		if call.ID != 12345 || call.FirstName != "Max" || call.LastName == nil || *call.LastName != "User" || call.Username == nil || *call.Username != "maxuser" || call.IsBot {
			t.Fatalf("unexpected UpsertUser params: %+v", call)
		}
	})

	t.Run("Valid signed X-Init-Data with nil optional fields", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":98765,"first_name":"Solo"}`,
		}, testBotToken)

		var recordedUserID int64
		handlerCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || !handlerCalled || recordedUserID != 98765 {
			t.Fatalf("expected status 200 and userID 98765, got code %d, id %d", rec.Code, recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 1 {
			t.Fatalf("expected 1 UpsertUser call, got %d", len(mockUserSvc.upsertCalls))
		}
		call := mockUserSvc.upsertCalls[0]
		if call.ID != 98765 || call.FirstName != "Solo" || call.LastName != nil || call.Username != nil {
			t.Fatalf("unexpected UpsertUser params: %+v", call)
		}
	})

	t.Run("Expired auth_date (> 1 hour) falls back to fake user ID 0 and skips UpsertUser", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		expiredTime := time.Now().Add(-2 * time.Hour).Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(expiredTime, 10),
			"user":      `{"id":12345,"first_name":"Max"}`,
		}, testBotToken)

		var recordedUserID int64
		handlerCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || !handlerCalled {
			t.Fatalf("expected status 200 and handler called, got code %d", rec.Code)
		}
		if recordedUserID != 0 {
			t.Fatalf("expected fallback userID 0, got %d", recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 0 {
			t.Fatalf("expected 0 UpsertUser calls, got %d", len(mockUserSvc.upsertCalls))
		}
	})

	t.Run("Invalid HMAC hash falls back to fake user ID 0 and skips UpsertUser", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":12345,"first_name":"Max"}`,
		}, testBotToken)

		// Tamper hash
		vals, _ := url.ParseQuery(initDataStr)
		vals.Set("hash", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		tamperedInitData := vals.Encode()

		var recordedUserID int64
		handlerCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", tamperedInitData)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if !handlerCalled || recordedUserID != 0 {
			t.Fatalf("expected handler called and fallback userID 0, got called=%v, id=%d", handlerCalled, recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 0 {
			t.Fatalf("expected 0 UpsertUser calls, got %d", len(mockUserSvc.upsertCalls))
		}
	})

	t.Run("Empty BOT_TOKEN falls back to fake user ID 0 without validation", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":12345,"first_name":"Max"}`,
		}, testBotToken)

		var recordedUserID int64
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware("", mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if recordedUserID != 0 {
			t.Fatalf("expected fallback userID 0 when botToken is empty, got %d", recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 0 {
			t.Fatalf("expected 0 UpsertUser calls, got %d", len(mockUserSvc.upsertCalls))
		}
	})

	t.Run("Missing X-Init-Data header falls back to fake user ID 0", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		var recordedUserID int64

		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if recordedUserID != 0 {
			t.Fatalf("expected fallback userID 0, got %d", recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 0 {
			t.Fatalf("expected 0 UpsertUser calls, got %d", len(mockUserSvc.upsertCalls))
		}
	})

	t.Run("initData in query parameter is strictly ignored", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":99999,"first_name":"QueryUser"}`,
		}, testBotToken)

		var recordedUserID int64
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test?initData="+url.QueryEscape(initDataStr), nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if recordedUserID != 0 {
			t.Fatalf("expected fallback userID 0, got %d", recordedUserID)
		}
		if len(mockUserSvc.upsertCalls) != 0 {
			t.Fatalf("expected 0 UpsertUser calls, got %d", len(mockUserSvc.upsertCalls))
		}
	})

	t.Run("UpsertUser failure aborts request with HTTP 500", func(t *testing.T) {
		mockUserSvc := &mockUserService{
			upsertUserFunc: func(_ context.Context, _ usecase.UpsertUserParams) error {
				return errors.New("db connection failure")
			},
		}
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":55555,"first_name":"FailedUser"}`,
		}, testBotToken)

		handlerCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", rec.Code)
		}
		if handlerCalled {
			t.Fatal("expected downstream handler NOT to be called when UpsertUser fails")
		}

		var errResp contracts.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("failed to decode error response json: %v", err)
		}
		if errResp.Message != "failed to upsert user" {
			t.Fatalf("expected message 'failed to upsert user', got %q", errResp.Message)
		}
	})

	t.Run("Nil userService does not panic and downstream handler receives user ID", func(t *testing.T) {
		now := time.Now().Unix()

		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      `{"id":77777,"first_name":"NilSvcUser"}`,
		}, testBotToken)

		var recordedUserID int64
		handlerCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, nil)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || !handlerCalled || recordedUserID != 77777 {
			t.Fatalf("expected status 200, handler called, and userID 77777, got code %d, id %d", rec.Code, recordedUserID)
		}
	})

	t.Run("Valid X-Init-Data with urlencoded JSON user", func(t *testing.T) {
		mockUserSvc := &mockUserService{}
		now := time.Now().Unix()

		// user is urlencoded
		userJSON := `{"id":67890,"username":"jane","first_name":"Jane"}`
		initDataStr := generateValidInitData(map[string]string{
			"auth_date": strconv.FormatInt(now, 10),
			"user":      userJSON,
		}, testBotToken)

		var recordedUserID int64
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			recordedUserID, _ = UserIDFromContext(r.Context())
		})

		middleware := AuthMiddleware(testBotToken, mockUserSvc)(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Init-Data", initDataStr)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if recordedUserID != 67890 {
			t.Fatalf("expected userID 67890, got %d", recordedUserID)
		}
	})
}
