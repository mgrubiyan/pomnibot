// Package http provides HTTP transport routing, handlers, and middlewares.
package http

import (
	"context"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

// APIHandler implements contracts.Handler.
type APIHandler struct{}

// NewAPIHandler constructs a new APIHandler.
func NewAPIHandler() *APIHandler {
	return &APIHandler{}
}

// GetHealth implements getHealth operation.
func (h *APIHandler) GetHealth(_ context.Context) (*contracts.HealthResponse, error) {
	return &contracts.HealthResponse{
		Status: "ok",
	}, nil
}
