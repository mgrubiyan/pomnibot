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
	ID int64 `json:"id"`
}

// AuthMiddleware extracts MAX WebApp user data exclusively from the X-Init-Data HTTP header.
// Query parameters are not inspected for initData. If valid user data is found, user.id is
// injected into context via WithUserID. If missing or invalid, it logs a warning via slog.Warn,
// injects fake ID 0 into context, and continues the handler chain.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawInitData := strings.TrimSpace(r.Header.Get("X-Init-Data"))
		if rawInitData == "" {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errMissingInitDataHeader)
			ctx := WithUserID(r.Context(), 0)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		vals, err := url.ParseQuery(rawInitData)
		if err != nil {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", err)
			ctx := WithUserID(r.Context(), 0)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		userRaw := vals.Get("user")
		if userRaw == "" {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errMissingUserField)
			ctx := WithUserID(r.Context(), 0)
			next.ServeHTTP(w, r.WithContext(ctx))
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
			ctx := WithUserID(r.Context(), 0)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if u.ID == 0 {
			slog.Warn("failed to extract user from MAX initData, using fake id 0", "error", errInvalidUserID)
			ctx := WithUserID(r.Context(), 0)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		ctx := WithUserID(r.Context(), u.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
