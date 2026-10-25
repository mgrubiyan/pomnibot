package http

import (
	"context"
	"errors"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// CardsHandler handles card transport operations.
type CardsHandler struct {
	cardService usecase.CardService
}

// NewCardsHandler creates a new CardsHandler.
func NewCardsHandler(cardService usecase.CardService) *CardsHandler {
	return &CardsHandler{
		cardService: cardService,
	}
}

// UpdateCard implements contracts.Handler.UpdateCard.
func (h *CardsHandler) UpdateCard(ctx context.Context, req *contracts.UpdateCardRequest, params contracts.UpdateCardParams) (contracts.UpdateCardRes, error) {
	if req == nil {
		return &contracts.UpdateCardBadRequest{Message: "request body is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.UpdateCard(ctx, userID, params.CardId, req)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.UpdateCardBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.UpdateCardNotFound{Message: err.Error()}, nil
		}
		return &contracts.UpdateCardInternalServerError{Message: internalError(ctx, "UpdateCard", err)}, nil
	}
	return res, nil
}

// DeleteCard implements contracts.Handler.DeleteCard.
func (h *CardsHandler) DeleteCard(ctx context.Context, params contracts.DeleteCardParams) (contracts.DeleteCardRes, error) {
	userID, _ := UserIDFromContext(ctx)
	err := h.cardService.DeleteCard(ctx, userID, params.CardId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.DeleteCardNotFound{Message: err.Error()}, nil
		}
		return &contracts.DeleteCardInternalServerError{Message: internalError(ctx, "DeleteCard", err)}, nil
	}
	return &contracts.DeleteCardNoContent{}, nil
}

// ReportCardIssue implements contracts.Handler.ReportCardIssue.
func (h *CardsHandler) ReportCardIssue(ctx context.Context, req *contracts.ReportCardIssueRequest, params contracts.ReportCardIssueParams) (contracts.ReportCardIssueRes, error) {
	if req == nil || req.Reason.Validate() != nil {
		return &contracts.ReportCardIssueBadRequest{Message: "reason is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	if err := h.cardService.ReportCardIssue(ctx, userID, params.CardId, req.Reason); err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.ReportCardIssueNotFound{Message: err.Error()}, nil
		}
		return &contracts.ReportCardIssueInternalServerError{Message: internalError(ctx, "ReportCardIssue", err)}, nil
	}
	return &contracts.ReportCardIssueNoContent{}, nil
}

// GetSetIssues implements contracts.Handler.GetSetIssues.
func (h *CardsHandler) GetSetIssues(ctx context.Context, params contracts.GetSetIssuesParams) (contracts.GetSetIssuesRes, error) {
	userID, _ := UserIDFromContext(ctx)
	issues, err := h.cardService.GetSetIssues(ctx, userID, params.SetId)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.GetSetIssuesNotFound{Message: err.Error()}, nil
		}
		return &contracts.GetSetIssuesInternalServerError{Message: internalError(ctx, "GetSetIssues", err)}, nil
	}
	res := contracts.GetSetIssuesOKApplicationJSON(issues)
	if res == nil {
		res = contracts.GetSetIssuesOKApplicationJSON{}
	}
	return &res, nil
}

// ResolveCardIssues implements contracts.Handler.ResolveCardIssues.
func (h *CardsHandler) ResolveCardIssues(ctx context.Context, params contracts.ResolveCardIssuesParams) (contracts.ResolveCardIssuesRes, error) {
	userID, _ := UserIDFromContext(ctx)
	if err := h.cardService.ResolveCardIssues(ctx, userID, params.CardId); err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.ResolveCardIssuesNotFound{Message: err.Error()}, nil
		}
		return &contracts.ResolveCardIssuesInternalServerError{Message: internalError(ctx, "ResolveCardIssues", err)}, nil
	}
	return &contracts.ResolveCardIssuesNoContent{}, nil
}

// AnswerQuestion implements contracts.Handler.AnswerQuestion.
func (h *CardsHandler) AnswerQuestion(ctx context.Context, req *contracts.AnswerQuestionRequest, params contracts.AnswerQuestionParams) (contracts.AnswerQuestionRes, error) {
	if req == nil || strings.TrimSpace(req.Answer) == "" {
		return &contracts.AnswerQuestionBadRequest{Message: "answer is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.AnswerQuestion(ctx, userID, params.CardId, strings.TrimSpace(req.Answer), req.AttemptId)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.AnswerQuestionBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.AnswerQuestionNotFound{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrConflict) {
			return &contracts.AnswerQuestionConflict{Message: err.Error()}, nil
		}
		return &contracts.AnswerQuestionInternalServerError{Message: internalError(ctx, "AnswerQuestion", err)}, nil
	}
	return res, nil
}

// RevealCard implements contracts.Handler.RevealCard.
func (h *CardsHandler) RevealCard(ctx context.Context, params contracts.RevealCardParams) (contracts.RevealCardRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.RevealCard(ctx, userID, params.CardId)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.RevealCardBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.RevealCardNotFound{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrConflict) {
			return &contracts.RevealCardConflict{Message: err.Error()}, nil
		}
		return &contracts.RevealCardInternalServerError{Message: internalError(ctx, "RevealCard", err)}, nil
	}
	return res, nil
}

// CreateCard creates manual content for the authenticated author.
func (h *CardsHandler) CreateCard(ctx context.Context, req *contracts.CreateCardRequest, params contracts.CreateCardParams) (contracts.CreateCardRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.CreateCard(ctx, userID, params.SetId, req)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.CreateCardBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.CreateCardNotFound{Message: err.Error()}, nil
		}
		return &contracts.CreateCardInternalServerError{Message: err.Error()}, nil
	}
	return res, nil
}

// CreateCardInFact creates manual card in an existing fact for the authenticated author.
func (h *CardsHandler) CreateCardInFact(ctx context.Context, req *contracts.CreateCardRequest, params contracts.CreateCardInFactParams) (contracts.CreateCardInFactRes, error) {
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.CreateCardInFact(ctx, userID, params.FactId, req)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.CreateCardInFactBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return &contracts.CreateCardInFactForbidden{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) {
			return &contracts.CreateCardInFactNotFound{Message: err.Error()}, nil
		}
		return &contracts.CreateCardInFactInternalServerError{Message: err.Error()}, nil
	}
	return res, nil
}
