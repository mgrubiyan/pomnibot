package http

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

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
	return LoggingMiddleware(authMiddleware(limitUploads(apiServer))), nil
}

// maxAvatarRequest fits the largest photo the API takes, in base64 and JSON.
const maxAvatarRequest = 512 << 10

// limitUploads stops reading an upload past its limit, before the whole body
// is held in memory. Notes get longer to arrive than the server's timeouts.
func limitUploads(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/profile/avatar":
			r.Body = http.MaxBytesReader(w, r.Body, maxAvatarRequest)
		case r.URL.Path == "/api/notes" && r.Method == http.MethodPost:
			if r.ContentLength > maxNotesRequest {
				writeAuthError(w, http.StatusRequestEntityTooLarge, "files over 20 MB in all", "")
				return
			}
			// Only a signed account may send notes. The rest are refused
			// before the body is read, so nobody can hold the server for
			// minutes or fill its memory without signing in.
			if _, ok := accountFromContext(r.Context()); !ok {
				writeAuthError(w, http.StatusForbidden, "authorization required", "")
				return
			}
			rc := http.NewResponseController(w)
			deadline := time.Now().Add(notesUploadTime)
			if err := errors.Join(rc.SetReadDeadline(deadline), rc.SetWriteDeadline(deadline)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				slog.Warn("notes upload keeps the server timeouts", "error", err)
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxNotesRequest)
		}
		next.ServeHTTP(w, r)
	})
}
