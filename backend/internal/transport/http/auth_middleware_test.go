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
	upsertUserFunc func(context.Context, usecase.UpsertUserParams) error
	ensureUserFunc func(context.Context, int64) error
	upsertCalls    []usecase.UpsertUserParams
	resolveFunc    func(context.Context, usecase.Identity) (usecase.Account, error)
}

func (m *mockUserService) UpsertUser(ctx context.Context, p usecase.UpsertUserParams) error {
	m.upsertCalls = append(m.upsertCalls, p)
	if m.upsertUserFunc != nil {
		return m.upsertUserFunc(ctx, p)
	}
	return nil
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
func (m *mockUserService) ResolveIdentity(ctx context.Context, i usecase.Identity) (usecase.Account, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, i)
	}
	p := usecase.UpsertUserParams{ID: i.ExternalID, FirstName: i.FirstName, LastName: &i.LastName, Username: &i.Username, IsBot: i.IsBot}
	if err := m.UpsertUser(ctx, p); err != nil {
		return usecase.Account{}, err
	}
	return usecase.Account{User: contracts.User{ID: i.ExternalID, FirstName: i.FirstName, LastName: contracts.NewOptString(i.LastName)}, Platform: i.Platform, ProfileComplete: true}, nil
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

type providerFunc func(context.Context, string) (usecase.Identity, error)

func (f providerFunc) Verify(ctx context.Context, raw string) (usecase.Identity, error) {
	return f(ctx, raw)
}

func TestAuthMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name, header                   string
		invalid, dbFailure, incomplete bool
		status                         int
		wantID                         int64
	}{
		{name: "max", header: "max original+%20data", status: 200, wantID: 900},
		{name: "telegram", header: "tma original+%20data", status: 200, wantID: 900},
		{name: "missing", status: 200}, {name: "unknown", header: "Bearer data", status: 200},
		{name: "invalid", header: "tma data", invalid: true, status: 200},
		{name: "database failure", header: "max data", dbFailure: true, status: 500},
		{name: "incomplete", header: "tma data", incomplete: true, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			makeProvider := func(platform string) IdentityProvider {
				return providerFunc(func(_ context.Context, raw string) (usecase.Identity, error) {
					calls = append(calls, platform)
					if raw != strings.SplitN(tc.header, " ", 2)[1] {
						t.Fatalf("raw changed: %q", raw)
					}
					if tc.invalid {
						return usecase.Identity{}, errors.New("invalid")
					}
					return usecase.Identity{Platform: platform, ExternalID: 42}, nil
				})
			}
			users := &mockUserService{resolveFunc: func(_ context.Context, i usecase.Identity) (usecase.Account, error) {
				if tc.dbFailure {
					return usecase.Account{}, errors.New("database unavailable")
				}
				return usecase.Account{User: contracts.User{ID: 900, FirstName: "Saved"}, Platform: i.Platform, ProfileComplete: !tc.incomplete}, nil
			}}
			called := false
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				called = true
				id, _ := UserIDFromContext(r.Context())
				if id != tc.wantID {
					t.Fatalf("id=%d", id)
				}
				name, _ := UserNameFromContext(r.Context())
				if id != 0 && name != "Saved" {
					t.Fatalf("name=%s", name)
				}
			})
			request := httptest.NewRequest("GET", "/api/sets", nil)
			request.Header.Set("Authorization", tc.header)
			request.Header.Set("X-Init-Data", "ignored")
			response := httptest.NewRecorder()
			AuthMiddleware(makeProvider("max"), makeProvider("telegram"), users)(next).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}
			if len(calls) > 1 {
				t.Fatal("called both providers")
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
