package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

const accountContextKey contextKey = "account"

func accountFromContext(ctx context.Context) (usecase.Account, bool) {
	account, ok := ctx.Value(accountContextKey).(usecase.Account)
	return account, ok
}

// AuthMiddleware selects exactly one provider by the Authorization scheme.
// Missing or invalid credentials retain the shared browser test account.
func AuthMiddleware(maxProvider, telegramProvider IdentityProvider, users usecase.AccountService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fallback := WithUserName(WithUserID(r.Context(), 0), "Browser Test User")
			scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			var provider IdentityProvider
			switch {
			case strings.EqualFold(scheme, "max"):
				provider = maxProvider
			case strings.EqualFold(scheme, "tma"):
				provider = telegramProvider
			}
			if !ok || raw == "" || provider == nil {
				next.ServeHTTP(w, r.WithContext(fallback))
				return
			}
			identity, err := provider.Verify(r.Context(), raw)
			if err != nil {
				next.ServeHTTP(w, r.WithContext(fallback))
				return
			}
			if users == nil {
				writeAuthError(w, 500, "account service unavailable", "")
				return
			}
			account, err := users.ResolveIdentity(r.Context(), identity)
			if err != nil {
				slog.ErrorContext(r.Context(), "resolve authenticated user", "error", err)
				writeAuthError(w, 500, "failed to resolve user", "")
				return
			}
			if account.Platform == "telegram" && !account.ProfileComplete && (r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")) && r.URL.Path != "/api/profile" && r.URL.Path != "/api/health" {
				writeAuthError(w, http.StatusForbidden, "Заполните фамилию и имя", "profile_required")
				return
			}
			name := strings.TrimSpace(account.User.FirstName + " " + account.User.LastName.Value)
			ctx := context.WithValue(WithUserName(WithUserID(r.Context(), account.User.ID), name), accountContextKey, account)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	response := contracts.ErrorResponse{Message: message}
	if code != "" {
		response.Code.SetTo(code)
	}
	_ = json.NewEncoder(w).Encode(response)
}
