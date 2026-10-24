package http

import (
	"context"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// HomescreenHandler handles homescreen and feed transport operations.
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
		return &contracts.GetTodayInternalServerError{Message: internalError(ctx, "GetToday", err)}, nil
	}
	return res, nil
}

// GetFeedQuestions implements contracts.Handler.GetFeedQuestions.
func (h *HomescreenHandler) GetFeedQuestions(ctx context.Context) (contracts.GetFeedQuestionsRes, error) {
	userID, _ := UserIDFromContext(ctx)
	cards, err := h.homescreenService.GetFeedQuestions(ctx, userID)
	if err != nil {
		return &contracts.GetFeedQuestionsInternalServerError{Message: internalError(ctx, "GetFeedQuestions", err)}, nil
	}
	if cards == nil {
		cards = []contracts.FeedCard{}
	}
	cardsRes := contracts.GetFeedQuestionsOKApplicationJSON(cards)
	return &cardsRes, nil
}
