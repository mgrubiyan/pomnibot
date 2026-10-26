package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

const accountContextKey contextKey = "account"

func accountFromContext(ctx context.Context) (usecase.Account, bool) {
	account, ok := ctx.Value(accountContextKey).(usecase.Account)
	return account, ok
}

// AuthMiddleware resolves a verified platform ID to the internal user ID.
// A request without platform credentials keeps the shared browser test
// account. Credentials that do not hold or have expired are refused with
// sign_in_expired: passing them on as the test account would record the
// reader's answers there and show them other people's sets.
// The messenger photo of a signed user is taken in the background by photos.
func AuthMiddleware(maxToken, telegramToken string, users usecase.UserService, photos *PhotoImporter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fallback := WithUserName(WithUserID(r.Context(), uuid.Nil), "Browser Test User")
			scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			var user *initDataUser
			var err error
			switch {
			case ok && raw != "" && strings.EqualFold(scheme, "max") && maxToken != "":
				user, err = validateInitData(raw, maxToken, time.Now())
			case ok && raw != "" && strings.EqualFold(scheme, "tma") && telegramToken != "":
				user, err = validateTelegramInitData(raw, telegramToken, time.Now())
			}
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "Вход устарел, откройте приложение заново", "sign_in_expired")
				return
			}
			if user == nil {
				next.ServeHTTP(w, r.WithContext(fallback))
				return
			}
			if users == nil {
				writeAuthError(w, 500, "user service unavailable", "")
				return
			}
			var account usecase.Account
			if strings.EqualFold(scheme, "max") {
				first := strings.TrimSpace(user.FirstName)
				if first == "" {
					first = resolveUserName(*user)
				}
				last, username := strings.TrimSpace(user.LastName), strings.TrimSpace(user.Username)
				account, err = users.UpsertMAXUser(r.Context(), usecase.MAXUserParams{MaxID: user.ID, FirstName: first, LastName: &last, Username: &username, IsBot: user.IsBot})
			} else {
				account, err = users.UpsertTelegramUser(r.Context(), usecase.TelegramUserParams{TgID: user.ID, Username: user.Username, IsBot: user.IsBot})
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "resolve authenticated user", "error", err)
				writeAuthError(w, 500, "failed to resolve user", "")
				return
			}
			photos.Sync(r.Context(), account, strings.TrimSpace(user.PhotoURL))
			if account.TgID != 0 && !account.ProfileComplete && (r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")) && r.URL.Path != "/api/profile" && r.URL.Path != "/api/health" {
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
