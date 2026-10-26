package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
)

// ManualWriter saves complete manual cards atomically.
type ManualWriter interface {
	CreateSet(context.Context, uuid.UUID, string, *contracts.CreateCardRequest) (uuid.UUID, error)
	CreateCard(context.Context, uuid.UUID, uuid.UUID, *contracts.CreateCardRequest) (uuid.UUID, error)
}

func validateCreateCard(req *contracts.CreateCardRequest) error {
	invalid := func() error { return fmt.Errorf("%w: invalid card fields", ErrValidation) }
	if req == nil || strings.TrimSpace(req.Question) == "" {
		return invalid()
	}
	switch req.Kind {
	case contracts.CardKindFlip, contracts.CardKindInput:
		if req.Answer.Type != contracts.StringCardAnswer || strings.TrimSpace(req.Answer.String) == "" {
			return invalid()
		}
	case contracts.CardKindBoolean:
		if req.Answer.Type != contracts.BoolCardAnswer {
			return invalid()
		}
	case contracts.CardKindChoice:
		if (req.Answer.Type != contracts.StringCardAnswer && req.Answer.Type != contracts.StringArrayCardAnswer) || len(req.Options) < 2 {
			return invalid()
		}
		seen := map[string]string{}
		for _, v := range req.Options {
			key := strings.TrimSpace(v)
			if _, exists := seen[key]; exists || key == "" {
				return invalid()
			}
			seen[key] = v
		}

		answers := req.Answer.StringArray
		if req.Answer.Type == contracts.StringCardAnswer {
			answers = []string{req.Answer.String}
		}
		if len(answers) == 0 {
			return invalid()
		}
		selected := map[string]bool{}
		for i, a := range answers {
			a = strings.TrimSpace(a)
			if _, exists := seen[a]; !exists || selected[a] {
				return invalid()
			}
			selected[a] = true
			answers[i] = seen[a]
		}
		if req.Answer.Type == contracts.StringCardAnswer {
			req.Answer.String = answers[0]
		} else {
			req.Answer.StringArray = answers
		}

	case contracts.CardKindTable:
		if !req.Table.IsSet() || req.Answer.Type != contracts.TableLayoutCardAnswer {
			return invalid()
		}
		table := req.Table.Value
		if len(table.Columns) < 2 {
			return invalid()
		}
		counts := map[string]int{}
		columnNames := map[string]string{}
		for _, v := range table.Columns {
			key := strings.TrimSpace(v)
			if _, ok := counts[key]; ok || key == "" {
				return invalid()
			}
			counts[key] = 0
			columnNames[key] = v
		}
		for i, v := range table.Items {
			key := strings.TrimSpace(v.Column)
			if _, ok := counts[key]; !ok || strings.TrimSpace(v.Text) == "" {
				return invalid()
			}
			counts[key]++
			v.Column = columnNames[key]
			table.Items[i] = v
		}
		for _, n := range counts {
			if n == 0 {
				return invalid()
			}
		}
		req.Table.SetTo(table)
		req.Answer = contracts.NewTableLayoutCardAnswer(table)
	default:
		return invalid()
	}
	return nil
}

func (s *setServiceImpl) CreateSet(ctx context.Context, userID uuid.UUID, req *contracts.CreateSetRequest) (*contracts.CardSet, error) {
	if req == nil || strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	var cardReq *contracts.CreateCardRequest
	if req.Card.IsSet() {
		if len(req.Card.Value.QuestionImageIds) > 0 || len(req.Card.Value.ExplanationImageIds) > 0 {
			return nil, fmt.Errorf("%w: new set has no images", ErrValidation)
		}
		cardReq = &req.Card.Value
		if err := validateCreateCard(cardReq); err != nil {
			return nil, err
		}
	}
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}
	if s.manual == nil {
		return nil, fmt.Errorf("manual writer is not configured")
	}
	id, err := s.manual.CreateSet(ctx, userID, strings.TrimSpace(req.Title), cardReq)
	if err != nil {
		return nil, err
	}
	return s.GetSet(ctx, userID, id)
}

func (s *cardServiceImpl) CreateCard(ctx context.Context, userID uuid.UUID, setID uuid.UUID, req *contracts.CreateCardRequest) (*contracts.Card, error) {
	if err := validateCreateCard(req); err != nil {
		return nil, err
	}
	if s.manual == nil {
		return nil, fmt.Errorf("manual writer is not configured")
	}
	id, err := s.manual.CreateCard(ctx, userID, setID, req)
	if errors.Is(err, repository.ErrManualSetNotFound) {
		return nil, ErrNotFound
	}
	if errors.Is(err, repository.ErrInvalidImage) {
		return nil, fmt.Errorf("%w: invalid image attachment", ErrValidation)
	}
	if err != nil {
		return nil, err
	}
	card := &contracts.Card{ID: id, SetId: setID, Kind: req.Kind, Question: req.Question, Answer: contracts.NewOptCardAnswer(req.Answer), Options: req.Options, Table: req.Table, Explanation: contracts.NewOptString(req.Explanation.Value)}
	if err := setResponseImages(ctx, s.querier, pgtype.UUID{Bytes: id, Valid: true}, card); err != nil {
		return nil, err
	}
	return card, nil
}

// WithManualCards configures transactional manual card persistence.
func WithManualCards(writer ManualWriter) CardServiceOption {
	return func(s *cardServiceImpl) { s.manual = writer }
}
