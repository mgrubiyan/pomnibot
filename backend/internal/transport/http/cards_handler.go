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
		return &contracts.UpdateCardInternalServerError{Message: err.Error()}, nil
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
		return &contracts.DeleteCardInternalServerError{Message: err.Error()}, nil
	}
	return &contracts.DeleteCardNoContent{}, nil
}

// AnswerQuestion implements contracts.Handler.AnswerQuestion.
func (h *CardsHandler) AnswerQuestion(ctx context.Context, req *contracts.AnswerQuestionRequest, params contracts.AnswerQuestionParams) (contracts.AnswerQuestionRes, error) {
	if req == nil || strings.TrimSpace(req.Answer) == "" {
		return &contracts.AnswerQuestionBadRequest{Message: "answer is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.AnswerQuestion(ctx, userID, params.CardId, strings.TrimSpace(req.Answer))
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.AnswerQuestionBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.AnswerQuestionNotFound{Message: err.Error()}, nil
		}
		return &contracts.AnswerQuestionInternalServerError{Message: err.Error()}, nil
	}
	return res, nil
}

// CheckAnswer implements contracts.Handler.CheckAnswer.
func (h *CardsHandler) CheckAnswer(ctx context.Context, req *contracts.CheckAnswerRequest, params contracts.CheckAnswerParams) (contracts.CheckAnswerRes, error) {
	if req == nil || strings.TrimSpace(req.Answer) == "" {
		return &contracts.CheckAnswerBadRequest{Message: "answer is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	res, err := h.cardService.CheckAnswer(ctx, userID, params.CardId, strings.TrimSpace(req.Answer))
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.CheckAnswerBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.CheckAnswerNotFound{Message: err.Error()}, nil
		}
		return &contracts.CheckAnswerInternalServerError{Message: err.Error()}, nil
	}
	return res, nil
}

// ReportCardIssue implements contracts.Handler.ReportCardIssue.
func (h *CardsHandler) ReportCardIssue(ctx context.Context, req *contracts.ReportCardIssueRequest, params contracts.ReportCardIssueParams) (contracts.ReportCardIssueRes, error) {
	if req == nil || strings.TrimSpace(string(req.Reason)) == "" {
		return &contracts.ReportCardIssueBadRequest{Message: "reason is required"}, nil
	}
	userID, _ := UserIDFromContext(ctx)
	err := h.cardService.ReportCardIssue(ctx, userID, params.CardId, req.Reason)
	if err != nil {
		if errors.Is(err, usecase.ErrValidation) {
			return &contracts.ReportCardIssueBadRequest{Message: err.Error()}, nil
		}
		if errors.Is(err, usecase.ErrNotFound) || errors.Is(err, usecase.ErrForbidden) {
			return &contracts.ReportCardIssueNotFound{Message: err.Error()}, nil
		}
		return &contracts.ReportCardIssueInternalServerError{Message: err.Error()}, nil
	}
	return &contracts.ReportCardIssueNoContent{}, nil
}
