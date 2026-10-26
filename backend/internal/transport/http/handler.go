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
	images     usecase.ImageService
	users      usecase.UserService
	sets       *SetsHandler
	cards      *CardsHandler
	facts      *FactsHandler
	homescreen *HomescreenHandler
	admin      usecase.AdminService
	notes      *noteJobs
	speech     SpeechRecognizer
	// browserProfile lets the browser test account change its profile.
	browserProfile bool
}

var _ contracts.Handler = (*APIHandler)(nil)

// NewAPIHandler constructs a new APIHandler with the given usecase services.
// Passing nil for any service safely falls back to contracts.UnimplementedHandler.
func NewAPIHandler(
	setService usecase.SetService,
	cardService usecase.CardService,
	homescreenService usecase.HomescreenService,
	users usecase.UserService,
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
		users:      users,
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

// UpdateSet implements contracts.Handler.UpdateSet.
func (h *APIHandler) UpdateSet(ctx context.Context, req *contracts.UpdateSetRequest, params contracts.UpdateSetParams) (contracts.UpdateSetRes, error) {
	if h.sets != nil {
		return h.sets.UpdateSet(ctx, req, params)
	}
	return h.UnimplementedHandler.UpdateSet(ctx, req, params)
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

// GetSetMistakes implements contracts.Handler.GetSetMistakes.
func (h *APIHandler) GetSetMistakes(ctx context.Context, params contracts.GetSetMistakesParams) (contracts.GetSetMistakesRes, error) {
	if h.sets != nil {
		return h.sets.GetSetMistakes(ctx, params)
	}
	return h.UnimplementedHandler.GetSetMistakes(ctx, params)
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

// RevealCard implements contracts.Handler.RevealCard.
func (h *APIHandler) RevealCard(ctx context.Context, params contracts.RevealCardParams) (contracts.RevealCardRes, error) {
	if h.cards != nil {
		return h.cards.RevealCard(ctx, params)
	}
	return h.UnimplementedHandler.RevealCard(ctx, params)
}

// AnswerQuestion implements contracts.Handler.AnswerQuestion.
func (h *APIHandler) AnswerQuestion(ctx context.Context, req *contracts.AnswerQuestionRequest, params contracts.AnswerQuestionParams) (contracts.AnswerQuestionRes, error) {
	if h.cards != nil {
		return h.cards.AnswerQuestion(ctx, req, params)
	}
	return h.UnimplementedHandler.AnswerQuestion(ctx, req, params)
}

// ReportCardIssue implements contracts.Handler.ReportCardIssue.
func (h *APIHandler) ReportCardIssue(ctx context.Context, req *contracts.ReportCardIssueRequest, params contracts.ReportCardIssueParams) (contracts.ReportCardIssueRes, error) {
	if h.cards != nil {
		return h.cards.ReportCardIssue(ctx, req, params)
	}
	return h.UnimplementedHandler.ReportCardIssue(ctx, req, params)
}

// GetSetIssues implements contracts.Handler.GetSetIssues.
func (h *APIHandler) GetSetIssues(ctx context.Context, params contracts.GetSetIssuesParams) (contracts.GetSetIssuesRes, error) {
	if h.cards != nil {
		return h.cards.GetSetIssues(ctx, params)
	}
	return h.UnimplementedHandler.GetSetIssues(ctx, params)
}

// ResolveCardIssues implements contracts.Handler.ResolveCardIssues.
func (h *APIHandler) ResolveCardIssues(ctx context.Context, params contracts.ResolveCardIssuesParams) (contracts.ResolveCardIssuesRes, error) {
	if h.cards != nil {
		return h.cards.ResolveCardIssues(ctx, params)
	}
	return h.UnimplementedHandler.ResolveCardIssues(ctx, params)
}

// --- Homescreen Domain Delegation ---

// GetToday implements contracts.Handler.GetToday.
func (h *APIHandler) GetToday(ctx context.Context) (contracts.GetTodayRes, error) {
	if h.homescreen == nil {
		return h.UnimplementedHandler.GetToday(ctx)
	}
	res, err := h.homescreen.GetToday(ctx)
	if today, ok := res.(*contracts.TodayData); ok && err == nil {
		h.markAdmin(ctx, today)
	}
	return res, err
}

// GetFeedQuestions implements contracts.Handler.GetFeedQuestions.
func (h *APIHandler) GetFeedQuestions(ctx context.Context, params contracts.GetFeedQuestionsParams) (contracts.GetFeedQuestionsRes, error) {
	if h.homescreen != nil {
		return h.homescreen.GetFeedQuestions(ctx, params)
	}
	return h.UnimplementedHandler.GetFeedQuestions(ctx, params)
}

// CreateSet delegates manual creation.
func (h *APIHandler) CreateSet(ctx context.Context, req *contracts.CreateSetRequest) (contracts.CreateSetRes, error) {
	if h.sets != nil {
		return h.sets.CreateSet(ctx, req)
	}
	return h.UnimplementedHandler.CreateSet(ctx, req)
}

// CreateCard delegates manual creation.
func (h *APIHandler) CreateCard(ctx context.Context, req *contracts.CreateCardRequest, params contracts.CreateCardParams) (contracts.CreateCardRes, error) {
	if h.cards != nil {
		return h.cards.CreateCard(ctx, req, params)
	}
	return h.UnimplementedHandler.CreateCard(ctx, req, params)
}

// CreateCardInFact delegates card creation in a fact.
func (h *APIHandler) CreateCardInFact(ctx context.Context, req *contracts.CreateCardRequest, params contracts.CreateCardInFactParams) (contracts.CreateCardInFactRes, error) {
	if h.cards != nil {
		return h.cards.CreateCardInFact(ctx, req, params)
	}
	return h.UnimplementedHandler.CreateCardInFact(ctx, req, params)
}

// CreateFact delegates fact creation.
func (h *APIHandler) CreateFact(ctx context.Context, req *contracts.CreateFactRequest, params contracts.CreateFactParams) (contracts.CreateFactRes, error) {
	if h.facts != nil {
		return h.facts.CreateFact(ctx, req, params)
	}
	return h.UnimplementedHandler.CreateFact(ctx, req, params)
}

// UpdateFact delegates fact update.
func (h *APIHandler) UpdateFact(ctx context.Context, req *contracts.UpdateFactRequest, params contracts.UpdateFactParams) (contracts.UpdateFactRes, error) {
	if h.facts != nil {
		return h.facts.UpdateFact(ctx, req, params)
	}
	return h.UnimplementedHandler.UpdateFact(ctx, req, params)
}

// DeleteFact delegates fact deletion.
func (h *APIHandler) DeleteFact(ctx context.Context, params contracts.DeleteFactParams) (contracts.DeleteFactRes, error) {
	if h.facts != nil {
		return h.facts.DeleteFact(ctx, params)
	}
	return h.UnimplementedHandler.DeleteFact(ctx, params)
}

// ReorderCards delegates cards reordering.
func (h *APIHandler) ReorderCards(ctx context.Context, req *contracts.ReorderCardsRequest, params contracts.ReorderCardsParams) (contracts.ReorderCardsRes, error) {
	if h.facts != nil {
		return h.facts.ReorderCards(ctx, req, params)
	}
	return h.UnimplementedHandler.ReorderCards(ctx, req, params)
}

// GetFactsBySetID delegates facts retrieval.
func (h *APIHandler) GetFactsBySetID(ctx context.Context, params contracts.GetFactsBySetIDParams) (contracts.GetFactsBySetIDRes, error) {
	if h.facts != nil {
		return h.facts.GetFactsBySetID(ctx, params)
	}
	return h.UnimplementedHandler.GetFactsBySetID(ctx, params)
}

// GetSetRetention implements contracts.Handler.GetSetRetention.
func (h *APIHandler) GetSetRetention(ctx context.Context, params contracts.GetSetRetentionParams) (contracts.GetSetRetentionRes, error) {
	if h.sets != nil {
		return h.sets.GetSetRetention(ctx, params)
	}
	return h.UnimplementedHandler.GetSetRetention(ctx, params)
}
