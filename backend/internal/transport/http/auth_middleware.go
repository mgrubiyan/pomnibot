package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

var (
	errMissingInitDataHeader = errors.New("missing X-Init-Data header")
	errMissingHash           = errors.New("missing hash in initData")
	errMissingAuthDate       = errors.New("missing auth_date in initData")
	errInvalidAuthDate       = errors.New("invalid auth_date in initData")
	errExpiredAuthDate       = errors.New("expired auth_date in initData")
	errHashMismatch          = errors.New("hash mismatch in initData")
	errMissingUserField      = errors.New("missing user in initData")
	errInvalidUserID         = errors.New("invalid or zero user ID")
)

const maxInitDataAge = time.Hour

type maxUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	IsBot     bool   `json:"is_bot"`
}

func resolveUserName(u maxUser) string {
	if strings.TrimSpace(u.Name) != "" {
		return strings.TrimSpace(u.Name)
	}
	fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if fullName != "" {
		return fullName
	}
	if strings.TrimSpace(u.FirstName) != "" {
		return strings.TrimSpace(u.FirstName)
	}
	if strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username)
	}
	return "MAX User"
}

func validateInitData(rawInitData, botToken string, now time.Time) (*maxUser, error) {
	raw := strings.TrimSpace(rawInitData)
	raw = strings.TrimPrefix(raw, "#")
	if strings.HasPrefix(raw, "WebAppData=") {
		raw = strings.TrimPrefix(raw, "WebAppData=")
		if unescaped, err := url.QueryUnescape(raw); err == nil {
			raw = unescaped
		}
	}

	vals, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}

	hash := vals.Get("hash")
	if hash == "" {
		return nil, errMissingHash
	}

	authDateStr := vals.Get("auth_date")
	if authDateStr == "" {
		return nil, errMissingAuthDate
	}

	authTimestamp, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, errInvalidAuthDate
	}

	age := now.Unix() - authTimestamp
	if age > int64(maxInitDataAge.Seconds()) {
		return nil, errExpiredAuthDate
	}
	if age < -300 {
		return nil, errInvalidAuthDate
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+vals.Get(k))
	}
	launchParams := strings.Join(lines, "\n")

	h := hmac.New(sha256.New, []byte("WebAppData"))
	h.Write([]byte(botToken))
	secretKey := h.Sum(nil)

	h2 := hmac.New(sha256.New, secretKey)
	h2.Write([]byte(launchParams))
	calcHashBytes := h2.Sum(nil)

	givenHashBytes, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(givenHashBytes, calcHashBytes) {
		return nil, errHashMismatch
	}

	userRaw := vals.Get("user")
	if userRaw == "" {
		return nil, errMissingUserField
	}

	var u maxUser
	if unmarshalErr := json.Unmarshal([]byte(userRaw), &u); unmarshalErr != nil {
		if unescaped, unescapeErr := url.QueryUnescape(userRaw); unescapeErr == nil {
			if unmarshalErr = json.Unmarshal([]byte(unescaped), &u); unmarshalErr != nil {
				return nil, unmarshalErr
			}
		} else {
			return nil, unmarshalErr
		}
	}

	if u.ID <= 0 {
		return nil, errInvalidUserID
	}

	return &u, nil
}

// AuthMiddleware extracts MAX WebApp user data exclusively from the X-Init-Data HTTP header.
// It cryptographically validates initData using botToken according to MAX specifications.
// If valid and u.ID > 0, it upserts the user via userService and injects user info into context.
// If missing or invalid, it logs a warning via slog.Warn, injects fake ID 0 and name "Browser Test User"
// into context, and continues the handler chain.
func AuthMiddleware(botToken string, userService usecase.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fallbackCtx := WithUserName(WithUserID(r.Context(), 0), "Browser Test User")

			if botToken == "" {
				slog.Warn("bot token is empty; skipping MAX initData validation, using fake id 0")
				next.ServeHTTP(w, r.WithContext(fallbackCtx))
				return
			}

			rawInitData := strings.TrimSpace(r.Header.Get("X-Init-Data"))
			if rawInitData == "" {
				slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errMissingInitDataHeader)
				next.ServeHTTP(w, r.WithContext(fallbackCtx))
				return
			}

			u, err := validateInitData(rawInitData, botToken, time.Now())
			if err != nil {
				slog.Warn("failed to validate MAX initData, using fake id 0", "error", err)
				next.ServeHTTP(w, r.WithContext(fallbackCtx))
				return
			}

			if userService != nil && u.ID > 0 {
				var lastName *string
				if strings.TrimSpace(u.LastName) != "" {
					l := strings.TrimSpace(u.LastName)
					lastName = &l
				}
				var username *string
				if strings.TrimSpace(u.Username) != "" {
					un := strings.TrimSpace(u.Username)
					username = &un
				}
				firstName := strings.TrimSpace(u.FirstName)
				if firstName == "" {
					firstName = resolveUserName(*u)
				}

				err := userService.UpsertUser(r.Context(), usecase.UpsertUserParams{
					ID:        u.ID,
					FirstName: firstName,
					LastName:  lastName,
					Username:  username,
					IsBot:     u.IsBot,
				})
				if err != nil {
					slog.ErrorContext(r.Context(), "failed to upsert user in database", "userID", u.ID, "error", err)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(contracts.ErrorResponse{Message: "failed to upsert user"})
					return
				}
			}

			ctx := WithUserName(WithUserID(r.Context(), u.ID), resolveUserName(*u))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// NewAuthMiddleware is an alias for AuthMiddleware.
func NewAuthMiddleware(botToken string, userService usecase.UserService) func(http.Handler) http.Handler {
	return AuthMiddleware(botToken, userService)
}
