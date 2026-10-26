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
	return authenticator{maxToken: maxToken, telegramToken: telegramToken, users: users, photos: photos}.middleware
}

// authenticator checks the Authorization of a request, or of a WebSocket's
// first message: browsers set no headers on a WebSocket.
type authenticator struct {
	maxToken, telegramToken string
	users                   usecase.UserService
	photos                  *PhotoImporter
}

// authFailure is a refusal, written as an ErrorResponse.
type authFailure struct {
	status        int
	message, code string
}

func (a authenticator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, fail := a.authenticate(r.Context(), r.Header.Get("Authorization"), r.URL.Path)
		if fail != nil {
			writeAuthError(w, fail.status, fail.message, fail.code)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate gives ctx the account the authorization belongs to, or the
// shared test account without one.
func (a authenticator) authenticate(ctx context.Context, authorization, path string) (context.Context, *authFailure) {
	fallback := WithUserName(WithUserID(ctx, uuid.Nil), "Browser Test User")
	scheme, raw, ok := strings.Cut(authorization, " ")
	var user *initDataUser
	var err error
	switch {
	case ok && raw != "" && strings.EqualFold(scheme, "max") && a.maxToken != "":
		user, err = validateInitData(raw, a.maxToken, time.Now())
	case ok && raw != "" && strings.EqualFold(scheme, "tma") && a.telegramToken != "":
		user, err = validateTelegramInitData(raw, a.telegramToken, time.Now())
	}
	if err != nil {
		return nil, &authFailure{http.StatusUnauthorized, "Вход устарел, откройте приложение заново", "sign_in_expired"}
	}
	if user == nil {
		return fallback, nil
	}
	if a.users == nil {
		return nil, &authFailure{500, "user service unavailable", ""}
	}
	var account usecase.Account
	if strings.EqualFold(scheme, "max") {
		first := strings.TrimSpace(user.FirstName)
		if first == "" {
			first = resolveUserName(*user)
		}
		last, username := strings.TrimSpace(user.LastName), strings.TrimSpace(user.Username)
		account, err = a.users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: user.ID, FirstName: first, LastName: &last, Username: &username, IsBot: user.IsBot})
	} else {
		account, err = a.users.UpsertTelegramUser(ctx, usecase.TelegramUserParams{TgID: user.ID, Username: user.Username, IsBot: user.IsBot})
	}
	if err != nil {
		slog.ErrorContext(ctx, "resolve authenticated user", "error", err)
		return nil, &authFailure{500, "failed to resolve user", ""}
	}
	a.photos.Sync(ctx, account, strings.TrimSpace(user.PhotoURL))
	if account.TgID != 0 && !account.ProfileComplete && (path == "/api" || strings.HasPrefix(path, "/api/")) && path != "/api/profile" && path != "/api/health" {
		return nil, &authFailure{http.StatusForbidden, "Заполните фамилию и имя", "profile_required"}
	}
	name := strings.TrimSpace(account.User.FirstName + " " + account.User.LastName.Value)
	return context.WithValue(WithUserName(WithUserID(ctx, account.User.ID), name), accountContextKey, account), nil
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
