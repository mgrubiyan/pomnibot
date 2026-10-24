package http

import (
	"context"
	"errors"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// FactsHandler handles fact transport operations.
type FactsHandler struct {
	factService usecase.FactService
}

// NewFactsHandler creates a new FactsHandler.
func NewFactsHandler(factService usecase.FactService) *FactsHandler {
	return &FactsHandler{
		factService: factService,
	}
}

// WithFacts configures FactService on APIHandler.
func (h *APIHandler) WithFacts(facts usecase.FactService) *APIHandler {
	if facts != nil {
		h.facts = NewFactsHandler(facts)
	}
	return h
}

// GetFactsBySetID implements contracts.Handler.GetFactsBySetID.
func (h *FactsHandler) GetFactsBySetID(ctx context.Context, params contracts.GetFactsBySetIDParams) (contracts.GetFactsBySetIDRes, error) {
	userID, _ := UserIDFromContext(ctx)
	facts, err := h.factService.GetFactsBySetID(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetFactsBySetIDForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrValidation) {
			return &contracts.GetFactsBySetIDNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetFactsBySetIDInternalServerError{Message: internalError(ctx, "GetFactsBySetID", err)}, nil
	}
	res := contracts.GetFactsBySetIDOKApplicationJSON(facts)
	return &res, nil
}

// CreateFact implements contracts.Handler.CreateFact.
func (h *FactsHandler) CreateFact(ctx context.Context, req *contracts.CreateFactRequest, params contracts.CreateFactParams) (contracts.CreateFactRes, error) {
	userID, _ := UserIDFromContext(ctx)
	if req == nil {
		return &contracts.CreateFactBadRequest{Message: "request body required"}, nil
	}
	fact, err := h.factService.CreateFact(ctx, userID, params.SetId, req.Name)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.CreateFactBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.CreateFactForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.CreateFactNotFound{Message: err.Error()}, nil
		}
		return &contracts.CreateFactInternalServerError{Message: internalError(ctx, "CreateFact", err)}, nil
	}
	return fact, nil
}

// UpdateFact implements contracts.Handler.UpdateFact.
func (h *FactsHandler) UpdateFact(ctx context.Context, req *contracts.UpdateFactRequest, params contracts.UpdateFactParams) (contracts.UpdateFactRes, error) {
	userID, _ := UserIDFromContext(ctx)
	if req == nil {
		return &contracts.UpdateFactBadRequest{Message: "request body required"}, nil
	}
	err := h.factService.UpdateFact(ctx, userID, params.FactId, req.Name)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.UpdateFactBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.UpdateFactForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.UpdateFactNotFound{Message: err.Error()}, nil
		}
		return &contracts.UpdateFactInternalServerError{Message: internalError(ctx, "UpdateFact", err)}, nil
	}
	return &contracts.UpdateFactNoContent{}, nil
}

// DeleteFact implements contracts.Handler.DeleteFact.
func (h *FactsHandler) DeleteFact(ctx context.Context, params contracts.DeleteFactParams) (contracts.DeleteFactRes, error) {
	userID, _ := UserIDFromContext(ctx)
	err := h.factService.DeleteFact(ctx, userID, params.FactId)
	if err != nil {
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.DeleteFactForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.DeleteFactNotFound{Message: err.Error()}, nil
		}
		return &contracts.DeleteFactInternalServerError{Message: internalError(ctx, "DeleteFact", err)}, nil
	}
	return &contracts.DeleteFactNoContent{}, nil
}

// ReorderCards implements contracts.Handler.ReorderCards.
func (h *FactsHandler) ReorderCards(ctx context.Context, req *contracts.ReorderCardsRequest, params contracts.ReorderCardsParams) (contracts.ReorderCardsRes, error) {
	userID, _ := UserIDFromContext(ctx)
	if req == nil {
		return &contracts.ReorderCardsBadRequest{Message: "request body required"}, nil
	}
	err := h.factService.ReorderCards(ctx, userID, params.FactId, req.CardIds)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.ReorderCardsBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.ReorderCardsForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.ReorderCardsNotFound{Message: err.Error()}, nil
		}
		return &contracts.ReorderCardsInternalServerError{Message: internalError(ctx, "ReorderCards", err)}, nil
	}
	return &contracts.ReorderCardsNoContent{}, nil
}
