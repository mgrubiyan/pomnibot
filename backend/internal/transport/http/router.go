package http

import (
	"io/fs"
	"net/http"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// NewRouter sets up the HTTP router combining OpenAPI handlers, SPA static file serving, and middlewares.
func NewRouter(handler contracts.Handler, staticFS fs.FS, botToken string, userService usecase.UserService) (http.Handler, error) {
	staticHandler := NewSPAHandler(staticFS)

	apiServer, err := contracts.NewServer(
		handler,
		contracts.WithPathPrefix("/api"),
		contracts.WithNotFound(staticHandler.ServeHTTP),
	)
	if err != nil {
		return nil, err
	}

	authMiddleware := AuthMiddleware(botToken, userService)
	return LoggingMiddleware(authMiddleware(apiServer)), nil
}
