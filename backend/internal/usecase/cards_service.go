package usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type cardServiceImpl struct {
	querier     db.Querier
	userService UserService
	grader      *grader.Grader
}

// CardServiceOption configures NewCardService.
type CardServiceOption func(*cardServiceImpl)

// WithGrader checks typed answers with g, which asks a language model about
// answers in other words. Without it answers are checked locally: form,
// word order and typos are forgiven, other words are not.
func WithGrader(g *grader.Grader) CardServiceOption {
	return func(s *cardServiceImpl) { s.grader = g }
}

// NewCardService creates a new CardService implementation backed by db.Querier.
func NewCardService(querier db.Querier, userService UserService, opts ...CardServiceOption) CardService {
	s := &cardServiceImpl{
		querier:     querier,
		userService: userService,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *cardServiceImpl) UpdateCard(ctx context.Context, userID int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request body is required", ErrValidation)
	}

	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	existing, err := s.querier.GetCardByIDForAuthor(ctx, db.GetCardByIDForAuthorParams{
		ID:       pgCardID,
		AuthorID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id for author: %w", err)
	}

	// Update base card fields
	arg := db.UpdateCardParams{
		ID:          pgCardID,
		Kind:        existing.Kind,
		Question:    existing.Question,
		AnswerText:  existing.AnswerText,
		Explanation: existing.Explanation,
		SourceQuote: existing.SourceQuote,
		SourceRef:   existing.SourceRef,
		AuthorID:    userID,
	}
	if req.Kind.IsSet() {
		arg.Kind = string(req.Kind.Value)
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
	if req.Answer.IsSet() {
		switch req.Answer.Value.Type {
		case contracts.StringCardAnswer:
			arg.AnswerText = pgtype.Text{String: req.Answer.Value.String, Valid: true}
		case contracts.BoolCardAnswer:
			arg.AnswerText = pgtype.Text{String: strconv.FormatBool(req.Answer.Value.Bool), Valid: true}
		case contracts.IntCardAnswer:
			if int(req.Answer.Value.Int) < len(req.Options) {
				arg.AnswerText = pgtype.Text{String: req.Options[req.Answer.Value.Int], Valid: true}
			} else {
				arg.AnswerText = pgtype.Text{String: strconv.Itoa(req.Answer.Value.Int), Valid: true}
			}
		case contracts.TableLayoutCardAnswer:
			// table answer text can remain as is
		}
	}

	updated, err := s.querier.UpdateCard(ctx, arg)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
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

	// If table layout is provided, replace columns and items
	if req.Table.IsSet() {
		_ = s.querier.DeleteCardTableColumns(ctx, pgCardID)
		_ = s.querier.DeleteCardTableItems(ctx, pgCardID)
		for idx, col := range req.Table.Value.Columns {
			_, _ = s.querier.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
				CardID:   pgCardID,
				Position: int32(idx),
				Name:     col,
			})
		}
		for _, item := range req.Table.Value.Items {
			_, _ = s.querier.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
				CardID:     pgCardID,
				ItemText:   item.Text,
				ColumnName: item.Column,
			})
		}
	}

	setUUID, _ := uuid.FromBytes(existing.SetID.Bytes[:])
	cardKind := contracts.CardKind(updated.Kind)
	topic := existing.Topic
	if req.Topic.IsSet() {
		topic = req.Topic.Value
	}

	res := &contracts.Card{
		ID:          cardID,
		SetId:       setUUID,
		Kind:        cardKind,
		Question:    updated.Question,
		Explanation: updated.Explanation,
		SourceQuote: updated.SourceQuote,
		Topic:       topic,
	}

	if updated.SourceRef.Valid && updated.SourceRef.String != "" {
		res.SourceRef.SetTo(updated.SourceRef.String)
	}

	if len(req.Options) > 0 {
		res.Options = req.Options
	}

	if req.Table.IsSet() {
		res.Table.SetTo(req.Table.Value)
		res.Answer = contracts.CardAnswer{
			Type:        contracts.TableLayoutCardAnswer,
			TableLayout: req.Table.Value,
		}
	} else if cardKind == contracts.CardKindBoolean {
		b, _ := strconv.ParseBool(updated.AnswerText.String)
		res.Answer = contracts.CardAnswer{
			Type: contracts.BoolCardAnswer,
			Bool: b,
		}
	} else {
		res.Answer = contracts.CardAnswer{
			Type:   contracts.StringCardAnswer,
			String: updated.AnswerText.String,
		}
	}

	return res, nil
}

func (s *cardServiceImpl) DeleteCard(ctx context.Context, userID int64, cardID uuid.UUID) error {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	rows, err := s.querier.DeleteCard(ctx, db.DeleteCardParams{
		ID:       pgCardID,
		AuthorID: userID,
	})
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *cardServiceImpl) AnswerQuestion(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	card, err := s.querier.GetCardByID(ctx, db.GetCardByIDParams{
		ID:     pgCardID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id: %w", err)
	}

	isCorrect := s.grader.Check(ctx, gradeInput(card, answer)).Correct

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

// CheckAnswer grades a typed answer by meaning without recording it: the feed
// sends its results with SendResults.
func (s *cardServiceImpl) CheckAnswer(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.CheckAnswerResponse, error) {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}
	card, err := s.querier.GetCardByID(ctx, db.GetCardByIDParams{ID: pgCardID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id: %w", err)
	}

	v := s.grader.Check(ctx, gradeInput(card, answer))
	res := &contracts.CheckAnswerResponse{
		IsCorrect: v.Correct,
		Method:    contracts.CheckAnswerResponseMethod(v.Method),
	}
	if v.Reason != "" {
		res.Reason = contracts.NewOptString(v.Reason)
	}
	return res, nil
}

func gradeInput(card db.GetCardByIDRow, answer string) grader.Input {
	return grader.Input{
		Question: card.Question,
		Expected: card.AnswerText.String,
		Quote:    card.SourceQuote,
		Given:    answer,
	}
}
