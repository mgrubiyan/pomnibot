// Package http provides HTTP transport routing, handlers, and middlewares.
package http

import (
	"context"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// APIHandler implements contracts.Handler by delegating to domain-specific handlers.
type APIHandler struct {
	contracts.UnimplementedHandler
	sets       *SetsHandler
	cards      *CardsHandler
	homescreen *HomescreenHandler
}

var _ contracts.Handler = (*APIHandler)(nil)

// NewAPIHandler constructs a new APIHandler with the given usecase services.
// Passing nil for any service safely falls back to contracts.UnimplementedHandler.
func NewAPIHandler(
	setService usecase.SetService,
	cardService usecase.CardService,
	homescreenService usecase.HomescreenService,
) *APIHandler {
	var sets *SetsHandler
	if setService != nil {
		sets = NewSetsHandler(setService)
	}
	var cards *CardsHandler
	if cardService != nil {
		cards = NewCardsHandler(cardService)
	}
	var homescreen *HomescreenHandler
	if homescreenService != nil {
		homescreen = NewHomescreenHandler(homescreenService)
	}
	return &APIHandler{
		sets:       sets,
		cards:      cards,
		homescreen: homescreen,
	}
}

// GetHealth implements getHealth operation.
func (h *APIHandler) GetHealth(_ context.Context) (contracts.GetHealthRes, error) {
	return &contracts.HealthResponse{
		Status: "ok",
	}, nil
}

// --- Sets Domain Delegation ---

// GetSet implements contracts.Handler.GetSet.
func (h *APIHandler) GetSet(ctx context.Context, params contracts.GetSetParams) (contracts.GetSetRes, error) {
	if h.sets != nil {
		return h.sets.GetSet(ctx, params)
	}
	return h.UnimplementedHandler.GetSet(ctx, params)
}

// DeleteSet implements contracts.Handler.DeleteSet.
func (h *APIHandler) DeleteSet(ctx context.Context, params contracts.DeleteSetParams) (contracts.DeleteSetRes, error) {
	if h.sets != nil {
		return h.sets.DeleteSet(ctx, params)
	}
	return h.UnimplementedHandler.DeleteSet(ctx, params)
}

// GetCardsBySetID implements contracts.Handler.GetCardsBySetID.
func (h *APIHandler) GetCardsBySetID(ctx context.Context, params contracts.GetCardsBySetIDParams) (contracts.GetCardsBySetIDRes, error) {
	if h.sets != nil {
		return h.sets.GetCardsBySetID(ctx, params)
	}
	return h.UnimplementedHandler.GetCardsBySetID(ctx, params)
}

// GetSetShareCode implements contracts.Handler.GetSetShareCode.
func (h *APIHandler) GetSetShareCode(ctx context.Context, params contracts.GetSetShareCodeParams) (contracts.GetSetShareCodeRes, error) {
	if h.sets != nil {
		return h.sets.GetSetShareCode(ctx, params)
	}
	return h.UnimplementedHandler.GetSetShareCode(ctx, params)
}

// GetSetPlan implements contracts.Handler.GetSetPlan.
func (h *APIHandler) GetSetPlan(ctx context.Context, params contracts.GetSetPlanParams) (contracts.GetSetPlanRes, error) {
	if h.sets != nil {
		return h.sets.GetSetPlan(ctx, params)
	}
	return h.UnimplementedHandler.GetSetPlan(ctx, params)
}

// GetSetLeaderboard implements contracts.Handler.GetSetLeaderboard.
func (h *APIHandler) GetSetLeaderboard(ctx context.Context, params contracts.GetSetLeaderboardParams) (contracts.GetSetLeaderboardRes, error) {
	if h.sets != nil {
		return h.sets.GetSetLeaderboard(ctx, params)
	}
	return h.UnimplementedHandler.GetSetLeaderboard(ctx, params)
}

// JoinSetByShareCode implements contracts.Handler.JoinSetByShareCode.
func (h *APIHandler) JoinSetByShareCode(ctx context.Context, req *contracts.JoinSetRequest) (contracts.JoinSetByShareCodeRes, error) {
	if h.sets != nil {
		return h.sets.JoinSetByShareCode(ctx, req)
	}
	return h.UnimplementedHandler.JoinSetByShareCode(ctx, req)
}

// --- Cards Domain Delegation ---

// UpdateCard implements contracts.Handler.UpdateCard.
func (h *APIHandler) UpdateCard(ctx context.Context, req *contracts.UpdateCardRequest, params contracts.UpdateCardParams) (contracts.UpdateCardRes, error) {
	if h.cards != nil {
		return h.cards.UpdateCard(ctx, req, params)
	}
	return h.UnimplementedHandler.UpdateCard(ctx, req, params)
}

// DeleteCard implements contracts.Handler.DeleteCard.
func (h *APIHandler) DeleteCard(ctx context.Context, params contracts.DeleteCardParams) (contracts.DeleteCardRes, error) {
	if h.cards != nil {
		return h.cards.DeleteCard(ctx, params)
	}
	return h.UnimplementedHandler.DeleteCard(ctx, params)
}

// CheckAnswer implements contracts.Handler.CheckAnswer.
func (h *APIHandler) CheckAnswer(ctx context.Context, req *contracts.CheckAnswerRequest, params contracts.CheckAnswerParams) (contracts.CheckAnswerRes, error) {
	if h.cards != nil {
		return h.cards.CheckAnswer(ctx, req, params)
	}
	return h.UnimplementedHandler.CheckAnswer(ctx, req, params)
}

// AnswerQuestion implements contracts.Handler.AnswerQuestion.
func (h *APIHandler) AnswerQuestion(ctx context.Context, req *contracts.AnswerQuestionRequest, params contracts.AnswerQuestionParams) (contracts.AnswerQuestionRes, error) {
	if h.cards != nil {
		return h.cards.AnswerQuestion(ctx, req, params)
	}
	return h.UnimplementedHandler.AnswerQuestion(ctx, req, params)
}

// --- Homescreen Domain Delegation ---

// GetToday implements contracts.Handler.GetToday.
func (h *APIHandler) GetToday(ctx context.Context) (contracts.GetTodayRes, error) {
	if h.homescreen != nil {
		return h.homescreen.GetToday(ctx)
	}
	return h.UnimplementedHandler.GetToday(ctx)
}

// GetFeedQuestions implements contracts.Handler.GetFeedQuestions.
func (h *APIHandler) GetFeedQuestions(ctx context.Context) (contracts.GetFeedQuestionsRes, error) {
	if h.homescreen != nil {
		return h.homescreen.GetFeedQuestions(ctx)
	}
	return h.UnimplementedHandler.GetFeedQuestions(ctx)
}

// SendResults implements contracts.Handler.SendResults.
func (h *APIHandler) SendResults(ctx context.Context, req contracts.SendResultsRequest) (contracts.SendResultsRes, error) {
	if h.homescreen != nil {
		return h.homescreen.SendResults(ctx, req)
	}
	return h.UnimplementedHandler.SendResults(ctx, req)
}
