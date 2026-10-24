package http

import (
	"context"
	"errors"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// SetsHandler handles card set transport operations.
type SetsHandler struct {
	setService usecase.SetService
}

// NewSetsHandler creates a new SetsHandler.
func NewSetsHandler(setService usecase.SetService) *SetsHandler {
	return &SetsHandler{
		setService: setService,
	}
}

// GetSet implements contracts.Handler.GetSet.
func (h *SetsHandler) GetSet(ctx context.Context, params contracts.GetSetParams) (contracts.GetSetRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.setService.GetSet(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetSetNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetSetInternalServerError{Message: internalError(ctx, "GetSet", err)}, nil
	}
	return res, nil
}

// DeleteSet implements contracts.Handler.DeleteSet.
func (h *SetsHandler) DeleteSet(ctx context.Context, params contracts.DeleteSetParams) (contracts.DeleteSetRes, error) {
	userID, _ := UserIDFromContext(ctx)
	err := h.setService.DeleteSet(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.DeleteSetNotFound{Message: err.Error()}, nil
		}
		return &contracts.DeleteSetInternalServerError{Message: internalError(ctx, "DeleteSet", err)}, nil
	}
	return &contracts.DeleteSetNoContent{}, nil
}

// UpdateSet implements contracts.Handler.UpdateSet.
func (h *SetsHandler) UpdateSet(ctx context.Context, req *contracts.UpdateSetRequest, params contracts.UpdateSetParams) (contracts.UpdateSetRes, error) {
	userID, _ := UserIDFromContext(ctx)
	if req == nil {
		return &contracts.UpdateSetBadRequest{Message: "request body required"}, nil
	}
	err := h.setService.UpdateSetTitle(ctx, userID, params.SetId, req.Title)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.UpdateSetBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.UpdateSetForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.UpdateSetNotFound{Message: err.Error()}, nil
		}
		return &contracts.UpdateSetInternalServerError{Message: internalError(ctx, "UpdateSet", err)}, nil
	}
	return &contracts.UpdateSetNoContent{}, nil
}

// GetCardsBySetID implements contracts.Handler.GetCardsBySetID.
func (h *SetsHandler) GetCardsBySetID(ctx context.Context, params contracts.GetCardsBySetIDParams) (contracts.GetCardsBySetIDRes, error) {
	userID, _ := UserIDFromContext(ctx)
	cards, err := h.setService.GetCardsBySetID(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetCardsBySetIDNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetCardsBySetIDInternalServerError{Message: internalError(ctx, "GetCardsBySetID", err)}, nil
	}
	cardsRes := contracts.GetCardsBySetIDOKApplicationJSON(cards)
	return &cardsRes, nil
}

// GetSetShareCode implements contracts.Handler.GetSetShareCode.
func (h *SetsHandler) GetSetShareCode(ctx context.Context, params contracts.GetSetShareCodeParams) (contracts.GetSetShareCodeRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.setService.GetSetShareCode(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetSetShareCodeNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetSetShareCodeInternalServerError{Message: internalError(ctx, "GetSetShareCode", err)}, nil
	}
	return res, nil
}

// GetSetPlan implements contracts.Handler.GetSetPlan.
func (h *SetsHandler) GetSetPlan(ctx context.Context, params contracts.GetSetPlanParams) (contracts.GetSetPlanRes, error) {
	userID, _ := UserIDFromContext(ctx)
	planItems, err := h.setService.GetSetPlan(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetSetPlanNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetSetPlanInternalServerError{Message: internalError(ctx, "GetSetPlan", err)}, nil
	}
	planRes := contracts.SetPlanResponse(planItems)
	return &planRes, nil
}

// GetSetLeaderboard implements contracts.Handler.GetSetLeaderboard.
func (h *SetsHandler) GetSetLeaderboard(ctx context.Context, params contracts.GetSetLeaderboardParams) (contracts.GetSetLeaderboardRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.setService.GetSetLeaderboard(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.GetSetLeaderboardForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.GetSetLeaderboardNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetSetLeaderboardInternalServerError{Message: internalError(ctx, "GetSetLeaderboard", err)}, nil
	}
	return res, nil
}

// JoinSetByShareCode implements contracts.Handler.JoinSetByShareCode.
func (h *SetsHandler) JoinSetByShareCode(ctx context.Context, req *contracts.JoinSetRequest) (contracts.JoinSetByShareCodeRes, error) {
	if req == nil || strings.TrimSpace(req.Code) == "" {
		return &contracts.JoinSetByShareCodeBadRequest{Message: "code is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	res, err := h.setService.JoinSetByShareCode(ctx, userID, strings.TrimSpace(req.Code))
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.JoinSetByShareCodeBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.JoinSetByShareCodeNotFound{Message: err.Error()}, nil
		}
		return &contracts.JoinSetByShareCodeInternalServerError{Message: internalError(ctx, "JoinSetByShareCode", err)}, nil
	}
	return res, nil
}

// CreateSet creates manual content for the authenticated author.
func (h *SetsHandler) CreateSet(ctx context.Context, req *contracts.CreateSetRequest) (contracts.CreateSetRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.setService.CreateSet(ctx, userID, req)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.CreateSetBadRequest{Message: err.Error()}, nil
		}
		return &contracts.CreateSetInternalServerError{Message: err.Error()}, nil
	}
	return res, nil
}
