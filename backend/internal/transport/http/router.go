package http

import (
	"io/fs"
	"net/http"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// NewRouter sets up the HTTP router combining OpenAPI handlers, SPA static file serving, and middlewares.
func NewRouter(handler contracts.Handler, staticFS fs.FS, maxToken, telegramToken string, userService usecase.UserService) (http.Handler, error) {
	staticHandler := NewSPAHandler(staticFS)

	apiServer, err := contracts.NewServer(
		handler,
		contracts.WithPathPrefix("/api"),
		contracts.WithNotFound(staticHandler.ServeHTTP),
	)
	if err != nil {
		return nil, err
	}

	authMiddleware := AuthMiddleware(maxToken, telegramToken, userService)
	return LoggingMiddleware(limitAvatarBody(authMiddleware(apiServer))), nil
}

// maxAvatarRequest fits the largest photo the API takes, in base64 and JSON.
const maxAvatarRequest = 512 << 10

// limitAvatarBody stops reading a photo upload past its limit, before the
// whole body is held in memory.
func limitAvatarBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/profile/avatar" {
			r.Body = http.MaxBytesReader(w, r.Body, maxAvatarRequest)
		}
		next.ServeHTTP(w, r)
	})
}
