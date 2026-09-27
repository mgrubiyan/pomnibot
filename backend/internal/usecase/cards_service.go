package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type cardServiceImpl struct {
	querier db.Querier
}

// NewCardService creates a new CardService implementation backed by db.Querier.
func NewCardService(querier db.Querier) CardService {
	return &cardServiceImpl{querier: querier}
}

func (s *cardServiceImpl) UpdateCard(ctx context.Context, _ int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request body is required", ErrValidation)
	}

	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	existing, err := s.querier.GetCardByID(ctx, pgCardID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id: %w", err)
	}

	// Update base card fields
	arg := db.UpdateCardParams{
		ID:          pgCardID,
		Question:    existing.Question,
		AnswerText:  existing.AnswerText,
		Explanation: existing.Explanation,
		SourceQuote: existing.SourceQuote,
		SourceRef:   existing.SourceRef,
	}
	if req.Question.IsSet() {
		arg.Question = req.Question.Value
	}
	if req.Explanation.IsSet() {
		arg.Explanation = req.Explanation.Value
	}
	if req.SourceQuote.IsSet() {
		arg.SourceQuote = req.SourceQuote.Value
	}
	if req.SourceRef.IsSet() {
		arg.SourceRef = pgtype.Text{String: req.SourceRef.Value, Valid: true}
	}

	updated, err := s.querier.UpdateCard(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}

	// If options are provided for choice card, replace them
	if len(req.Options) > 0 {
		_ = s.querier.DeleteCardOptions(ctx, pgCardID)
		for idx, opt := range req.Options {
			isCorrect := strings.TrimSpace(opt) == strings.TrimSpace(updated.AnswerText.String)
			_, _ = s.querier.CreateCardOption(ctx, db.CreateCardOptionParams{
				CardID:    pgCardID,
				Position:  int32(idx),
				Text:      opt,
				IsCorrect: isCorrect,
			})
		}
	}

	setUUID, _ := uuid.FromBytes(existing.SetID.Bytes[:])
	cardKind := contracts.CardKind(updated.Kind)
	res := &contracts.Card{
		ID:          cardID,
		SetId:       setUUID,
		Kind:        cardKind,
		Question:    updated.Question,
		Explanation: updated.Explanation,
		SourceQuote: updated.SourceQuote,
		Topic:       existing.Topic,
	}

	if updated.SourceRef.Valid && updated.SourceRef.String != "" {
		res.SourceRef.SetTo(updated.SourceRef.String)
	}

	if len(req.Options) > 0 {
		res.Options = req.Options
	}

	res.Answer = contracts.CardAnswer{
		Type:   contracts.StringCardAnswer,
		String: updated.AnswerText.String,
	}

	return res, nil
}

func (s *cardServiceImpl) DeleteCard(ctx context.Context, _ int64, cardID uuid.UUID) error {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	_, err := s.querier.GetCardByID(ctx, pgCardID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get card by id: %w", err)
	}

	if err := s.querier.DeleteCard(ctx, pgCardID); err != nil {
		return fmt.Errorf("delete card: %w", err)
	}

	return nil
}

func (s *cardServiceImpl) AnswerQuestion(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	if err := EnsureUser(ctx, s.querier, userID, ""); err != nil {
		return nil, err
	}

	card, err := s.querier.GetCardByID(ctx, pgCardID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id: %w", err)
	}

	expectedAnswer := strings.TrimSpace(card.AnswerText.String)
	actualAnswer := strings.TrimSpace(answer)
	isCorrect := strings.EqualFold(expectedAnswer, actualAnswer)

	now := time.Now()
	var answeredAt pgtype.Timestamptz
	_ = answeredAt.Scan(now)

	_, err = s.querier.RecordAnswerResult(ctx, db.RecordAnswerResultParams{
		UserID:     userID,
		CardID:     pgCardID,
		Correct:    isCorrect,
		AnsweredAt: answeredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("record answer result: %w", err)
	}

	_, err = s.querier.UpdateFactProgressOnAnswer(ctx, db.UpdateFactProgressOnAnswerParams{
		UserID:  userID,
		FactID:  card.FactID,
		Column3: isCorrect,
	})
	if err != nil {
		return nil, fmt.Errorf("update fact progress on answer: %w", err)
	}

	return &contracts.AnswerQuestionResponse{
		IsCorrect:  isCorrect,
		UserAnswer: answer,
	}, nil
}

func (s *cardServiceImpl) ReportCardIssue(ctx context.Context, userID int64, cardID uuid.UUID, reason contracts.CardIssueReason) error {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	if err := EnsureUser(ctx, s.querier, userID, ""); err != nil {
		return err
	}

	_, err := s.querier.GetCardByID(ctx, pgCardID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get card by id: %w", err)
	}

	dbReason := db.CardIssueReason(reason)
	_, err = s.querier.CreateCardIssue(ctx, db.CreateCardIssueParams{
		CardID: pgCardID,
		UserID: userID,
		Reason: dbReason,
	})
	if err != nil {
		return fmt.Errorf("create card issue: %w", err)
	}

	return nil
}
