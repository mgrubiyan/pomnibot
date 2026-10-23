package http

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func profileResponse(account usecase.Account) *contracts.ProfileResponse {
	response := &contracts.ProfileResponse{User: account.User, ProfileComplete: account.ProfileComplete}
	if account.TgID != 0 {
		response.Platform.SetTo(contracts.ProfileResponsePlatformTelegram)
	} else if account.MaxID != 0 {
		response.Platform.SetTo(contracts.ProfileResponsePlatformMax)
	}
	return response
}

// GetProfile returns saved names, never unverified SDK profile data.
func (h *APIHandler) GetProfile(ctx context.Context) (contracts.GetProfileRes, error) {
	if account, ok := accountFromContext(ctx); ok {
		return profileResponse(account), nil
	}
	return profileResponse(usecase.Account{User: contracts.User{ID: uuid.Nil, FirstName: "Browser Test User"}, ProfileComplete: true}), nil
}

// PutProfile is restricted to verified Telegram accounts.
func (h *APIHandler) PutProfile(ctx context.Context, req *contracts.ProfileRequest) (contracts.PutProfileRes, error) {
	account, ok := accountFromContext(ctx)
	if !ok || account.TgID == 0 {
		return &contracts.PutProfileForbidden{Message: "Telegram authorization required"}, nil
	}
	if h.users == nil {
		return &contracts.PutProfileInternalServerError{Message: "profile service unavailable"}, nil
	}
	saved, err := h.users.SaveProfile(ctx, account.User.ID, req.FirstName, req.LastName)
	if errors.Is(err, usecase.ErrValidation) {
		return &contracts.PutProfileBadRequest{Message: err.Error()}, nil
	}
	if err != nil {
		return &contracts.PutProfileInternalServerError{Message: internalError(ctx, "PutProfile", err)}, nil
	}
	return profileResponse(saved), nil
}
