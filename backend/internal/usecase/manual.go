package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
)

// ManualWriter saves complete manual cards atomically.
type ManualWriter interface {
	CreateSet(context.Context, int64, string, *contracts.CreateCardRequest) (uuid.UUID, error)
	CreateCard(context.Context, int64, uuid.UUID, *contracts.CreateCardRequest) (uuid.UUID, error)
}

func validateCreateCard(req *contracts.CreateCardRequest) error {
	invalid := func() error { return fmt.Errorf("%w: invalid card fields", ErrValidation) }
	if req == nil || strings.TrimSpace(req.Question) == "" {
		return invalid()
	}
	req.Question = strings.TrimSpace(req.Question)
	switch req.Kind {
	case contracts.CardKindFlip, contracts.CardKindInput:
		if req.Answer.Type != contracts.StringCardAnswer || strings.TrimSpace(req.Answer.String) == "" {
			return invalid()
		}
		req.Answer.String = strings.TrimSpace(req.Answer.String)
	case contracts.CardKindBoolean:
		if req.Answer.Type != contracts.BoolCardAnswer {
			return invalid()
		}
	case contracts.CardKindChoice:
		if req.Answer.Type != contracts.StringCardAnswer || len(req.Options) < 2 {
			return invalid()
		}
		seen := map[string]bool{}
		for i, v := range req.Options {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				return invalid()
			}
			seen[v] = true
			req.Options[i] = v
		}
		req.Answer.String = strings.TrimSpace(req.Answer.String)
		if !seen[req.Answer.String] {
			return invalid()
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
		for i, v := range table.Columns {
			v = strings.TrimSpace(v)
			if _, ok := counts[v]; ok || v == "" {
				return invalid()
			}
			counts[v] = 0
			table.Columns[i] = v
		}
		for i, v := range table.Items {
			v.Text = strings.TrimSpace(v.Text)
			v.Column = strings.TrimSpace(v.Column)
			if _, ok := counts[v.Column]; !ok || v.Text == "" {
				return invalid()
			}
			counts[v.Column]++
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

func (s *setServiceImpl) CreateSet(ctx context.Context, userID int64, req *contracts.CreateSetRequest) (*contracts.CardSet, error) {
	if req == nil || strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	if err := validateCreateCard(&req.Card); err != nil {
		return nil, err
	}
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}
	if s.manual == nil {
		return nil, fmt.Errorf("manual writer is not configured")
	}
	id, err := s.manual.CreateSet(ctx, userID, strings.TrimSpace(req.Title), &req.Card)
	if err != nil {
		return nil, err
	}
	return s.GetSet(ctx, userID, id)
}

func (s *cardServiceImpl) CreateCard(ctx context.Context, userID int64, setID uuid.UUID, req *contracts.CreateCardRequest) (*contracts.Card, error) {
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
	if err != nil {
		return nil, err
	}
	return &contracts.Card{ID: id, SetId: setID, Kind: req.Kind, Question: req.Question, Answer: req.Answer, Options: req.Options, Table: req.Table, Explanation: req.Explanation.Value}, nil
}

// WithManualCards configures transactional manual card persistence.
func WithManualCards(writer ManualWriter) CardServiceOption {
	return func(s *cardServiceImpl) { s.manual = writer }
}
