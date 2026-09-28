package http

import (
	"context"
	"errors"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// HomescreenHandler handles homescreen, feed, and results transport operations.
type HomescreenHandler struct {
	homescreenService usecase.HomescreenService
}

// NewHomescreenHandler creates a new HomescreenHandler.
func NewHomescreenHandler(homescreenService usecase.HomescreenService) *HomescreenHandler {
	return &HomescreenHandler{
		homescreenService: homescreenService,
	}
}

// GetToday implements contracts.Handler.GetToday.
func (h *HomescreenHandler) GetToday(ctx context.Context) (contracts.GetTodayRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.homescreenService.GetToday(ctx, userID)
	if err != nil {
		return &contracts.ErrorResponse{Message: err.Error()}, nil
	}
	return res, nil
}

// GetFeedQuestions implements contracts.Handler.GetFeedQuestions.
func (h *HomescreenHandler) GetFeedQuestions(ctx context.Context) (contracts.GetFeedQuestionsRes, error) {
	userID, _ := UserIDFromContext(ctx)
	cards, err := h.homescreenService.GetFeedQuestions(ctx, userID)
	if err != nil {
		return &contracts.ErrorResponse{Message: err.Error()}, nil
	}
	if cards == nil {
		cards = []contracts.Card{}
	}
	cardsRes := contracts.GetFeedQuestionsOKApplicationJSON(cards)
	return &cardsRes, nil
}

// SendResults implements contracts.Handler.SendResults.
func (h *HomescreenHandler) SendResults(ctx context.Context, req contracts.SendResultsRequest) (contracts.SendResultsRes, error) {
	if len(req) == 0 {
		return &contracts.SendResultsBadRequest{Message: "results are required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	err := h.homescreenService.SendResults(ctx, userID, []contracts.AnswerResult(req))
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) || errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.SendResultsBadRequest{Message: err.Error()}, nil
		}
		return &contracts.SendResultsInternalServerError{Message: err.Error()}, nil
	}
	return &contracts.SendResultsNoContent{}, nil
}
