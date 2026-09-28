package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

var (
	errMissingInitDataHeader = errors.New("missing X-Init-Data header")
	errMissingUserField      = errors.New("missing user in initData")
	errInvalidUserID         = errors.New("invalid or zero user ID")
)

type maxUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Name      string `json:"name"`
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

// AuthMiddleware extracts MAX WebApp user data exclusively from the X-Init-Data HTTP header.
// Query parameters are not inspected for initData. If valid user data is found, user.id is
// injected into context via WithUserID and user name via WithUserName. If missing or invalid, it logs a warning via slog.Warn,
// injects fake ID 0 and name "Browser Test User" into context, and continues the handler chain.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCtx := WithUserName(WithUserID(r.Context(), 0), "Browser Test User")

		rawInitData := strings.TrimSpace(r.Header.Get("X-Init-Data"))
		if rawInitData == "" {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errMissingInitDataHeader)
			next.ServeHTTP(w, r.WithContext(fallbackCtx))
			return
		}

		vals, err := url.ParseQuery(rawInitData)
		if err != nil {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", err)
			next.ServeHTTP(w, r.WithContext(fallbackCtx))
			return
		}

		userRaw := vals.Get("user")
		if userRaw == "" {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errMissingUserField)
			next.ServeHTTP(w, r.WithContext(fallbackCtx))
			return
		}

		var (
			u            maxUser
			unmarshalErr error
		)
		if unmarshalErr = json.Unmarshal([]byte(userRaw), &u); unmarshalErr != nil {
			// Try unescaping once if the user payload was doubly url-encoded
			if unescaped, unescapeErr := url.QueryUnescape(userRaw); unescapeErr == nil {
				unmarshalErr = json.Unmarshal([]byte(unescaped), &u)
			}
		}

		if unmarshalErr != nil {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", unmarshalErr)
			next.ServeHTTP(w, r.WithContext(fallbackCtx))
			return
		}

		if u.ID == 0 {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errInvalidUserID)
			next.ServeHTTP(w, r.WithContext(fallbackCtx))
			return
		}

		ctx := WithUserName(WithUserID(r.Context(), u.ID), resolveUserName(u))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
