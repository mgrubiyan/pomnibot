package http

import (
	"io/fs"
	"net/http"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// NewRouter sets up the HTTP router combining OpenAPI handlers, SPA static file serving, and middlewares.
func NewRouter(handler contracts.Handler, staticFS fs.FS, maxProvider, telegramProvider IdentityProvider, userService usecase.AccountService) (http.Handler, error) {
	staticHandler := NewSPAHandler(staticFS)

	apiServer, err := contracts.NewServer(
		handler,
		contracts.WithPathPrefix("/api"),
		contracts.WithNotFound(staticHandler.ServeHTTP),
	)
	if err != nil {
		return nil, err
	}

	authMiddleware := AuthMiddleware(maxProvider, telegramProvider, userService)
	return LoggingMiddleware(authMiddleware(apiServer)), nil
}
