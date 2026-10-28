package http

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func profileResponse(account usecase.Account) *contracts.ProfileResponse {
	response := &contracts.ProfileResponse{User: account.User,
		DataConsent: account.DataConsent, DataConsentAnswered: account.DataConsentAnswered}
	if account.MaxID != 0 {
		response.Platform.SetTo(contracts.ProfileResponsePlatformMax)
	}
	return response
}

// WithBrowserProfile lets the browser test account keep a profile of its
// own. Only for local development: in production a request without a
// signature is that shared account.
func (h *APIHandler) WithBrowserProfile() *APIHandler {
	h.browserProfile = true
	return h
}

// profileAccount returns the account whose profile the request may change.
func (h *APIHandler) profileAccount(ctx context.Context) (usecase.Account, bool, error) {
	if account, ok := accountFromContext(ctx); ok {
		return account, true, nil
	}
	if !h.browserProfile || h.users == nil {
		return usecase.Account{}, false, nil
	}
	if err := h.users.EnsureUser(ctx, uuid.Nil); err != nil {
		return usecase.Account{}, false, err
	}
	account, err := h.users.GetAccount(ctx, uuid.Nil)
	return account, err == nil, err
}

// GetProfile returns saved names, never unverified SDK profile data.
func (h *APIHandler) GetProfile(ctx context.Context) (contracts.GetProfileRes, error) {
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.ErrorResponse{Message: internalError(ctx, "GetProfile", err)}, nil
	}
	if ok {
		return profileResponse(account), nil
	}
	// Nobody can answer for the shared account, so it is never asked.
	return profileResponse(usecase.Account{User: contracts.User{ID: uuid.Nil, FirstName: "Browser Test User"}, DataConsentAnswered: true}), nil
}

// PutProfileConsent is restricted to verified accounts.
func (h *APIHandler) PutProfileConsent(ctx context.Context, req *contracts.ConsentRequest) (contracts.PutProfileConsentRes, error) {
	if h.users == nil {
		return &contracts.PutProfileConsentInternalServerError{Message: "profile service unavailable"}, nil
	}
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.PutProfileConsentInternalServerError{Message: internalError(ctx, "PutProfileConsent", err)}, nil
	}
	if !ok {
		return &contracts.PutProfileConsentForbidden{Message: "authorization required"}, nil
	}
	saved, err := h.users.SetDataConsent(ctx, account.User.ID, req.Given)
	if err != nil {
		return &contracts.PutProfileConsentInternalServerError{Message: internalError(ctx, "PutProfileConsent", err)}, nil
	}
	return profileResponse(saved), nil
}

// PutProfileAvatar is restricted to verified accounts.
func (h *APIHandler) PutProfileAvatar(ctx context.Context, req *contracts.AvatarRequest) (contracts.PutProfileAvatarRes, error) {
	if h.users == nil {
		return &contracts.PutProfileAvatarInternalServerError{Message: "profile service unavailable"}, nil
	}
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.PutProfileAvatarInternalServerError{Message: internalError(ctx, "PutProfileAvatar", err)}, nil
	}
	if !ok {
		return &contracts.PutProfileAvatarForbidden{Message: "authorization required"}, nil
	}
	saved, err := h.users.SaveAvatar(ctx, account.User.ID, req.Image)
	if errors.Is(err, usecase.ErrValidation) {
		return &contracts.PutProfileAvatarBadRequest{Message: err.Error()}, nil
	}
	if err != nil {
		return &contracts.PutProfileAvatarInternalServerError{Message: internalError(ctx, "PutProfileAvatar", err)}, nil
	}
	return profileResponse(saved), nil
}

// DeleteProfileAvatar is restricted to verified accounts.
func (h *APIHandler) DeleteProfileAvatar(ctx context.Context) (contracts.DeleteProfileAvatarRes, error) {
	if h.users == nil {
		return &contracts.DeleteProfileAvatarInternalServerError{Message: "profile service unavailable"}, nil
	}
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.DeleteProfileAvatarInternalServerError{Message: internalError(ctx, "DeleteProfileAvatar", err)}, nil
	}
	if !ok {
		return &contracts.DeleteProfileAvatarForbidden{Message: "authorization required"}, nil
	}
	saved, err := h.users.DeleteAvatar(ctx, account.User.ID)
	if err != nil {
		return &contracts.DeleteProfileAvatarInternalServerError{Message: internalError(ctx, "DeleteProfileAvatar", err)}, nil
	}
	return profileResponse(saved), nil
}

// GetAvatar serves a photo to anyone holding its address: a new photo gets
// a new one, so it may be cached for good.
func (h *APIHandler) GetAvatar(ctx context.Context, params contracts.GetAvatarParams) (contracts.GetAvatarRes, error) {
	if h.users == nil {
		return &contracts.GetAvatarInternalServerError{Message: "profile service unavailable"}, nil
	}
	photo, err := h.users.AvatarImage(ctx, params.AvatarId)
	if errors.Is(err, usecase.ErrNotFound) {
		return &contracts.GetAvatarNotFound{Message: "photo not found"}, nil
	}
	if err != nil {
		return &contracts.GetAvatarInternalServerError{Message: internalError(ctx, "GetAvatar", err)}, nil
	}
	return &contracts.GetAvatarOKHeaders{
		// Private: a shared cache would keep serving the photo after it is
		// deleted or its address changes.
		CacheControl: contracts.NewOptString("private, max-age=31536000, immutable"),
		Response:     contracts.GetAvatarOK{Data: bytes.NewReader(photo)},
	}, nil
}
