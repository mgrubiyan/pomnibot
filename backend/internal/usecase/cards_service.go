package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type cardServiceImpl struct {
	manual      ManualWriter
	querier     db.Querier
	userService UserService
	grader      AnswerGrader
	issues      IssueNotifier
	locks       *answerLocks
	tx          Transactor
}

// CardServiceOption configures NewCardService.
type CardServiceOption func(*cardServiceImpl)

// WithGrader checks typed answers with g, which asks a language model about
// answers in other words. Without it answers are checked locally: form,
// word order and typos are forgiven, other words are not.
func WithGrader(g AnswerGrader) CardServiceOption {
	return func(s *cardServiceImpl) { s.grader = g }
}

// Transactor runs writes in one database transaction.
type Transactor interface {
	InTx(ctx context.Context, fn func(db.Querier) error) error
}

// WithTransactions records an answer and the fact's next review in one
// transaction. Without it they are written one after the other.
func WithTransactions(t Transactor) CardServiceOption {
	return func(s *cardServiceImpl) { s.tx = t }
}

// NewCardService creates a new CardService implementation backed by db.Querier.
func NewCardService(querier db.Querier, userService UserService, opts ...CardServiceOption) CardService {
	s := &cardServiceImpl{
		grader:      LocalAnswerGrader{},
		locks:       &answerLocks{},
		querier:     querier,
		userService: userService,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *cardServiceImpl) UpdateCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request body is required", ErrValidation)
	}

	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	existing, err := s.querier.GetCardByIDForAuthor(ctx, db.GetCardByIDForAuthorParams{
		ID:       pgCardID,
		AuthorID: pgUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get card by id for author: %w", err)
	}

	// Validate a complete choice update before changing stored answers or options.
	if (req.Kind.IsSet() && req.Kind.Value == contracts.CardKindChoice) || (!req.Kind.IsSet() && existing.Kind == "choice") {
		if req.Answer.IsSet() || req.Options != nil {
			options := req.Options
			if options == nil {
				rows, loadErr := s.querier.GetCardOptions(ctx, pgCardID)
				if loadErr != nil {
					return nil, loadErr
				}
				for _, o := range rows {
					options = append(options, o.Text)
				}
			}
			answer := contracts.NewStringArrayCardAnswer(cards.ChoiceAnswers(existing.AnswerText.String))
			if req.Answer.IsSet() {
				answer = req.Answer.Value
			}
			if answer.Type == contracts.IntCardAnswer {
				if answer.Int < 0 || answer.Int >= len(options) {
					return nil, fmt.Errorf("%w: invalid choice index", ErrValidation)
				}
				answer = contracts.NewStringCardAnswer(options[answer.Int])
			}
			check := contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: existing.Question, Options: options, Answer: answer}
			if req.Question.IsSet() {
				check.Question = req.Question.Value
			}
			if err := validateCreateCard(&check); err != nil {
				return nil, err
			}
			req.Options = check.Options
			req.Answer = contracts.NewOptCardAnswer(check.Answer)
		}
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
		AuthorID:    pgUserID,
	}
	if req.Kind.IsSet() {
		arg.Kind = db.CardKind(req.Kind.Value)
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
		case contracts.StringArrayCardAnswer:
			data, _ := json.Marshal(req.Answer.Value.StringArray)
			arg.AnswerText = pgtype.Text{String: string(data), Valid: true}
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
		if err := s.querier.DeleteCardOptions(ctx, pgCardID); err != nil {
			return nil, fmt.Errorf("delete card options: %w", err)
		}
		for idx, opt := range req.Options {
			isCorrect := slices.Contains(cards.ChoiceAnswers(updated.AnswerText.String), strings.TrimSpace(opt))
			if _, err := s.querier.CreateCardOption(ctx, db.CreateCardOptionParams{
				CardID:    pgCardID,
				Position:  int32(idx),
				Text:      opt,
				IsCorrect: isCorrect,
			}); err != nil {
				return nil, fmt.Errorf("create card option %d: %w", idx, err)
			}
		}
	}

	// If table layout is provided, replace columns and items
	if req.Table.IsSet() {
		if err := s.querier.DeleteCardTableColumns(ctx, pgCardID); err != nil {
			return nil, fmt.Errorf("delete card table columns: %w", err)
		}
		if err := s.querier.DeleteCardTableItems(ctx, pgCardID); err != nil {
			return nil, fmt.Errorf("delete card table items: %w", err)
		}
		for idx, col := range req.Table.Value.Columns {
			if _, err := s.querier.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
				CardID:   pgCardID,
				Position: int32(idx),
				Name:     col,
			}); err != nil {
				return nil, fmt.Errorf("create card table column %d: %w", idx, err)
			}
		}
		for idx, item := range req.Table.Value.Items {
			if _, err := s.querier.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
				CardID:     pgCardID,
				ItemText:   item.Text,
				ColumnName: item.Column,
				Position:   int32(idx),
			}); err != nil {
				return nil, fmt.Errorf("create card table item %d: %w", idx, err)
			}
		}
	}

	setUUID, _ := uuid.FromBytes(existing.SetID.Bytes[:])
	factUUID, _ := uuid.FromBytes(existing.FactID.Bytes[:])
	cardKind := contracts.CardKind(updated.Kind)

	res := &contracts.Card{
		ID:          cardID,
		SetId:       setUUID,
		FactId:      factUUID,
		FactName:    existing.FactName,
		Kind:        cardKind,
		Question:    updated.Question,
		Explanation: contracts.NewOptString(updated.Explanation),
		SourceQuote: contracts.NewOptString(updated.SourceQuote),
	}

	if updated.SourceRef.Valid && updated.SourceRef.String != "" {
		res.SourceRef.SetTo(updated.SourceRef.String)
	}
	setCardIllustrations(res, updated.QuestionSvg, updated.ExplanationSvg)

	if len(req.Options) > 0 {
		res.Options = req.Options
	}

	if req.Table.IsSet() {
		res.Table.SetTo(req.Table.Value)
		res.Answer.SetTo(contracts.NewTableLayoutCardAnswer(req.Table.Value))
	} else if cardKind == contracts.CardKindChoice {
		res.Answer.SetTo(contracts.NewStringArrayCardAnswer(cards.ChoiceAnswers(updated.AnswerText.String)))
	} else if cardKind == contracts.CardKindBoolean {
		b, _ := strconv.ParseBool(updated.AnswerText.String)
		res.Answer.SetTo(contracts.NewBoolCardAnswer(b))
	} else {
		res.Answer.SetTo(contracts.NewStringCardAnswer(updated.AnswerText.String))
	}

	return res, nil
}

func (s *cardServiceImpl) DeleteCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error {
	var pgCardID pgtype.UUID
	if err := pgCardID.Scan(cardID.String()); err != nil {
		return fmt.Errorf("%w: invalid card id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	rows, err := s.querier.DeleteCard(ctx, db.DeleteCardParams{
		ID:       pgCardID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// ReportCardIssue saves a complaint about a card the user can see, together
// with a copy of the card, so it outlives edits and deletion. The author
// hears about the first reader's complaint until they answer it.
func (s *cardServiceImpl) ReportCardIssue(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, reason contracts.CardIssueReason, comment string) error {
	pgComment, err := cleanIssueComment(reason, comment)
	if err != nil {
		return err
	}
	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	pgCardID := pgtype.UUID{Bytes: cardID, Valid: true}

	saved, err := s.querier.CreateCardIssue(ctx, db.CreateCardIssueParams{
		UserID:  pgUserID,
		Reason:  db.CardIssueReason(reason),
		Comment: pgComment,
		CardID:  pgCardID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("create card issue: %w", err)
	}

	// Roll back reader's step so they don't skip ahead to harder cards.
	if err := s.querier.RollbackFactStepOnCardIssue(ctx, db.RollbackFactStepOnCardIssueParams{
		CardID: pgCardID,
		UserID: pgUserID,
	}); err != nil {
		return fmt.Errorf("rollback fact step: %w", err)
	}

	if saved.NotifyAuthor && s.issues != nil {
		s.issues.CardIssueReported(ctx, CardIssueReport{AuthorID: uuid.UUID(saved.AuthorID.Bytes), SetTitle: saved.SetTitle})
	}
	return nil
}

// cleanIssueComment keeps the reader's words without surrounding spaces, at
// most 500 characters; none is NULL. «Другое» means nothing without them.
func cleanIssueComment(reason contracts.CardIssueReason, comment string) (pgtype.Text, error) {
	comment = strings.TrimSpace(comment)
	if !utf8.ValidString(comment) || utf8.RuneCountInString(comment) > 500 {
		return pgtype.Text{}, fmt.Errorf("%w: comment must be at most 500 characters", ErrValidation)
	}
	if comment == "" && reason == contracts.CardIssueReasonOther {
		return pgtype.Text{}, fmt.Errorf("%w: say what is wrong with the card", ErrValidation)
	}
	return pgtype.Text{String: comment, Valid: comment != ""}, nil
}

// GetSetIssues lists the open complaints of the author's set; for anyone
// else the set is not found.
func (s *cardServiceImpl) GetSetIssues(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetIssue, error) {
	pgSetID := pgtype.UUID{Bytes: setID, Valid: true}
	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{ID: pgSetID, AuthorID: pgUserID})
	if err != nil {
		return nil, fmt.Errorf("check set author: %w", err)
	}
	if !isAuthor {
		return nil, ErrNotFound
	}
	rows, err := s.querier.GetSetIssues(ctx, db.GetSetIssuesParams{SetID: pgSetID, UserID: pgUserID})
	if err != nil {
		return nil, fmt.Errorf("get open card issues: %w", err)
	}
	issues := make([]contracts.SetIssue, 0, len(rows))
	for _, r := range rows {
		issues = append(issues, contracts.SetIssue{
			CardId:         uuid.UUID(r.CardID.Bytes),
			Reason:         contracts.CardIssueReason(r.Reason),
			Count:          int(r.Count),
			Comments:       r.Comments,
			LastReportedAt: r.LastReportedAt.Time,
		})
	}
	return issues, nil
}

// ResolveCardIssues closes the open complaints of a card the author keeps
// as it is. The complaints themselves stay.
func (s *cardServiceImpl) ResolveCardIssues(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error {
	found, err := s.querier.CloseCardIssues(ctx, db.CloseCardIssuesParams{
		CardID: pgtype.UUID{Bytes: cardID, Valid: true},
		UserID: pgtype.UUID{Bytes: userID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("resolve card issues: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (s *cardServiceImpl) CreateCardInFact(ctx context.Context, userID, factID uuid.UUID, req *contracts.CreateCardRequest) (*contracts.Card, error) {
	if err := validateCreateCard(req); err != nil {
		return nil, err
	}

	var pgFactUUID pgtype.UUID
	if err := pgFactUUID.Scan(factID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid fact id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsFactAuthor(ctx, db.IsFactAuthorParams{
		ID:       pgFactUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("check fact author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.FactExists(ctx, pgFactUUID)
		if err != nil {
			return nil, fmt.Errorf("check fact exists: %w", err)
		}
		if exists {
			return nil, ErrForbidden
		}
		return nil, ErrNotFound
	}

	maxStep, err := s.querier.GetMaxCardStepInFact(ctx, pgFactUUID)
	if err != nil {
		return nil, fmt.Errorf("get max card step: %w", err)
	}
	nextStep := maxStep + 1

	cardUUID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate card uuid: %w", err)
	}
	pgCardUUID := pgtype.UUID{Bytes: cardUUID, Valid: true}

	answer := req.Answer.String
	if req.Answer.Type == contracts.StringArrayCardAnswer {
		data, _ := json.Marshal(req.Answer.StringArray)
		answer = string(data)
	}
	if req.Kind == contracts.CardKindBoolean {
		answer = strconv.FormatBool(req.Answer.Bool)
	}

	created, err := s.querier.CreateCard(ctx, db.CreateCardParams{
		ID:          pgCardUUID,
		FactID:      pgFactUUID,
		Kind:        db.CardKind(req.Kind),
		Question:    req.Question,
		AnswerText:  pgtype.Text{String: answer, Valid: true},
		Explanation: req.Explanation.Value,
		Step:        nextStep,
	})
	if err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}

	if req.Kind == contracts.CardKindChoice {
		for i, v := range req.Options {
			if _, err := s.querier.CreateCardOption(ctx, db.CreateCardOptionParams{
				CardID:    pgCardUUID,
				Position:  int32(i),
				Text:      v,
				IsCorrect: v == answer || slices.Contains(req.Answer.StringArray, v),
			}); err != nil {
				return nil, fmt.Errorf("create card option: %w", err)
			}
		}
	}
	if req.Kind == contracts.CardKindTable {
		for i, v := range req.Table.Value.Columns {
			if _, err := s.querier.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
				CardID:   pgCardUUID,
				Position: int32(i),
				Name:     v,
			}); err != nil {
				return nil, fmt.Errorf("create card table column: %w", err)
			}
		}
		for i, v := range req.Table.Value.Items {
			if _, err := s.querier.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
				CardID:     pgCardUUID,
				ItemText:   v.Text,
				ColumnName: v.Column,
				Position:   int32(i),
			}); err != nil {
				return nil, fmt.Errorf("create card table item: %w", err)
			}
		}
	}

	res := &contracts.Card{
		ID:          cardUUID,
		FactId:      factID,
		Kind:        req.Kind,
		Question:    created.Question,
		Answer:      contracts.NewOptCardAnswer(req.Answer),
		Options:     req.Options,
		Table:       req.Table,
		Explanation: contracts.NewOptString(created.Explanation),
	}
	res.Step.SetTo(int(nextStep))
	return res, nil
}
