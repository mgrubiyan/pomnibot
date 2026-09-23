package http

import (
	"io/fs"
	"net/http"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

// NewRouter sets up the HTTP router combining OpenAPI handlers, SPA static file serving, and middlewares.
func NewRouter(handler contracts.Handler, staticFS fs.FS) (http.Handler, error) {
	staticHandler := NewSPAHandler(staticFS)

	apiServer, err := contracts.NewServer(
		handler,
		contracts.WithNotFound(staticHandler.ServeHTTP),
	)
	if err != nil {
		return nil, err
	}

	return LoggingMiddleware(apiServer), nil
}
