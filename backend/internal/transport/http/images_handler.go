package http

import (
	"bytes"
	"context"
	"errors"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// WithImages configures authorized PDF image access.
func (h *APIHandler) WithImages(s usecase.ImageService) *APIHandler { h.images = s; return h }

// GetSetImages returns the authorized set image catalog.
func (h *APIHandler) GetSetImages(ctx context.Context, p contracts.GetSetImagesParams) (contracts.GetSetImagesRes, error) {
	if h.images == nil {
		return h.UnimplementedHandler.GetSetImages(ctx, p)
	}
	userID, _ := UserIDFromContext(ctx)
	images, err := h.images.GetSetImages(ctx, userID, p.SetId)
	if errors.Is(err, usecase.ErrNotFound) {
		return &contracts.GetSetImagesNotFound{Message: "set not found"}, nil
	}
	if err != nil {
		return &contracts.GetSetImagesInternalServerError{Message: internalError(ctx, "GetSetImages", err)}, nil
	}
	result := contracts.GetSetImagesOKApplicationJSON(images)
	return &result, nil
}

// GetCardImage serves a crop after checking membership of its set.
func (h *APIHandler) GetCardImage(ctx context.Context, p contracts.GetCardImageParams) (contracts.GetCardImageRes, error) {
	if h.images == nil {
		return h.UnimplementedHandler.GetCardImage(ctx, p)
	}
	userID, _ := UserIDFromContext(ctx)
	data, err := h.images.GetCardImage(ctx, userID, p.AssetId)
	if errors.Is(err, usecase.ErrNotFound) {
		return &contracts.GetCardImageNotFound{Message: "image not found"}, nil
	}
	if err != nil {
		return &contracts.GetCardImageInternalServerError{Message: internalError(ctx, "GetCardImage", err)}, nil
	}
	return &contracts.GetCardImageOKHeaders{CacheControl: contracts.NewOptString("private, no-store"), Response: contracts.GetCardImageOK{Data: bytes.NewReader(data)}}, nil
}
