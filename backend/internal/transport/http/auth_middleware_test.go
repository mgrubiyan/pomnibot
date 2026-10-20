package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockUserService struct {
	usecase.UserService
	ensureUserFunc func(context.Context, int64) error
	upsertCalls    []usecase.MAXUserParams
	maxFunc        func(context.Context, usecase.MAXUserParams) (usecase.Account, error)
	telegramFunc   func(context.Context, usecase.TelegramUserParams) (usecase.Account, error)
}

func (m *mockUserService) EnsureUser(ctx context.Context, id int64) error {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, id)
	}
	return nil
}
func (m *mockUserService) GetUsersWithDueFacts(context.Context, time.Time, string) ([]int64, error) {
	return nil, nil
}
func (m *mockUserService) UpsertMAXUser(ctx context.Context, p usecase.MAXUserParams) (usecase.Account, error) {
	if m.maxFunc != nil {
		return m.maxFunc(ctx, p)
	}
	m.upsertCalls = append(m.upsertCalls, p)
	return usecase.Account{User: contracts.User{ID: p.MaxID + 100000, FirstName: p.FirstName}, MaxID: p.MaxID, ProfileComplete: true}, nil
}
func (m *mockUserService) UpsertTelegramUser(ctx context.Context, p usecase.TelegramUserParams) (usecase.Account, error) {
	return m.telegramFunc(ctx, p)
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
	for _, tc := range []struct {
		name, scheme                   string
		invalid, dbFailure, incomplete bool
		status                         int
		wantID                         int64
	}{
		{name: "max", scheme: "max", status: 200, wantID: 900},
		{name: "telegram", scheme: "tma", status: 200, wantID: 901},
		{name: "missing", status: 200}, {name: "unknown", scheme: "Bearer", status: 200},
		{name: "invalid", scheme: "tma", invalid: true, status: 200},
		{name: "database failure", scheme: "max", dbFailure: true, status: 500},
		{name: "incomplete", scheme: "tma", incomplete: true, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			account := func(platform string, id int64) (usecase.Account, error) {
				calls++
				if tc.dbFailure {
					return usecase.Account{}, errors.New("database unavailable")
				}
				saved := usecase.Account{User: contracts.User{ID: id, FirstName: "Saved"}, ProfileComplete: !tc.incomplete}
				if platform == "max" {
					saved.MaxID = 42
				} else {
					saved.TgID = 42
				}
				return saved, nil
			}
			users := &mockUserService{
				maxFunc: func(_ context.Context, p usecase.MAXUserParams) (usecase.Account, error) {
					if p.MaxID != 42 {
						t.Fatalf("MAX external ID: %d", p.MaxID)
					}
					return account("max", 900)
				},
				telegramFunc: func(_ context.Context, p usecase.TelegramUserParams) (usecase.Account, error) {
					if p.TgID != 42 {
						t.Fatalf("Telegram external ID: %d", p.TgID)
					}
					return account("telegram", 901)
				},
			}
			called := false
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true
				id, _ := UserIDFromContext(r.Context())
				if id != tc.wantID {
					t.Fatalf("internal id=%d, want %d", id, tc.wantID)
				}
				name, _ := UserNameFromContext(r.Context())
				if id != 0 && name != "Saved" {
					t.Fatalf("name=%s", name)
				}
			})
			raw := generateValidInitData(map[string]string{"auth_date": fmt.Sprint(time.Now().Unix()), "user": `{"id":42,"first_name":"Remote","username":"same"}`}, "secret")
			if tc.invalid {
				raw += "tamper"
			}
			request := httptest.NewRequest("GET", "/api/sets", nil)
			if tc.scheme != "" {
				request.Header.Set("Authorization", tc.scheme+" "+raw)
			}
			request.Header.Set("X-Init-Data", "ignored")
			response := httptest.NewRecorder()
			AuthMiddleware("secret", "secret", users)(next).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}
			wantCalls := 0
			if tc.scheme == "max" || tc.scheme == "tma" && !tc.invalid {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("upsert calls=%d, want %d", calls, wantCalls)
			}
			if tc.incomplete && !strings.Contains(response.Body.String(), "profile_required") {
				t.Fatal(response.Body.String())
			}
			if called != (tc.status == 200) {
				t.Fatal("unexpected handler dispatch")
			}
		})
	}
}
