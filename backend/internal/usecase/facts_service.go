package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type factServiceImpl struct {
	querier db.Querier
}

// NewFactService creates a new FactService implementation.
func NewFactService(querier db.Querier) FactService {
	return &factServiceImpl{
		querier: querier,
	}
}

func (s *factServiceImpl) GetFactsBySetID(ctx context.Context, userID, setID uuid.UUID) ([]contracts.Fact, error) {
	var pgSetUUID pgtype.UUID
	if err := pgSetUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgSetUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("check set author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.SetExists(ctx, pgSetUUID)
		if err != nil {
			return nil, fmt.Errorf("check set exists: %w", err)
		}
		if exists {
			return nil, ErrForbidden
		}
		return nil, ErrNotFound
	}

	rows, err := s.querier.GetFactsBySetID(ctx, pgSetUUID)
	if err != nil {
		return nil, fmt.Errorf("get facts by set id: %w", err)
	}

	facts := make([]contracts.Fact, 0, len(rows))
	for _, r := range rows {
		var fID uuid.UUID
		_ = fID.Scan(r.ID.Bytes[:])
		facts = append(facts, contracts.Fact{
			ID:    fID,
			SetId: setID,
			Name:  r.Name,
		})
	}
	return facts, nil
}

func (s *factServiceImpl) CreateFact(ctx context.Context, userID, setID uuid.UUID, name string) (*contracts.Fact, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: fact name cannot be empty", ErrValidation)
	}

	var pgSetUUID pgtype.UUID
	if err := pgSetUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgSetUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("check set author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.SetExists(ctx, pgSetUUID)
		if err != nil {
			return nil, fmt.Errorf("check set exists: %w", err)
		}
		if exists {
			return nil, ErrForbidden
		}
		return nil, ErrNotFound
	}

	factUUID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate fact uuid: %w", err)
	}
	pgFactUUID := pgtype.UUID{Bytes: factUUID, Valid: true}

	created, err := s.querier.CreateFact(ctx, db.CreateFactParams{
		ID:    pgFactUUID,
		SetID: pgSetUUID,
		Name:  trimmed,
	})
	if err != nil {
		return nil, fmt.Errorf("create fact: %w", err)
	}

	return &contracts.Fact{
		ID:    factUUID,
		SetId: setID,
		Name:  created.Name,
	}, nil
}

func (s *factServiceImpl) UpdateFact(ctx context.Context, userID, factID uuid.UUID, name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: fact name cannot be empty", ErrValidation)
	}

	var pgFactUUID pgtype.UUID
	if err := pgFactUUID.Scan(factID.String()); err != nil {
		return fmt.Errorf("%w: invalid fact id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsFactAuthor(ctx, db.IsFactAuthorParams{
		ID:       pgFactUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("check fact author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.FactExists(ctx, pgFactUUID)
		if err != nil {
			return fmt.Errorf("check fact exists: %w", err)
		}
		if exists {
			return ErrForbidden
		}
		return ErrNotFound
	}

	_, err = s.querier.UpdateFact(ctx, db.UpdateFactParams{
		ID:   pgFactUUID,
		Name: trimmed,
	})
	if err != nil {
		return fmt.Errorf("update fact: %w", err)
	}
	return nil
}

func (s *factServiceImpl) DeleteFact(ctx context.Context, userID, factID uuid.UUID) error {
	var pgFactUUID pgtype.UUID
	if err := pgFactUUID.Scan(factID.String()); err != nil {
		return fmt.Errorf("%w: invalid fact id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsFactAuthor(ctx, db.IsFactAuthorParams{
		ID:       pgFactUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("check fact author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.FactExists(ctx, pgFactUUID)
		if err != nil {
			return fmt.Errorf("check fact exists: %w", err)
		}
		if exists {
			return ErrForbidden
		}
		return ErrNotFound
	}

	rows, err := s.querier.DeleteFact(ctx, pgFactUUID)
	if err != nil {
		return fmt.Errorf("delete fact: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *factServiceImpl) ReorderCards(ctx context.Context, userID, factID uuid.UUID, cardIDs []uuid.UUID) error {
	var pgFactUUID pgtype.UUID
	if err := pgFactUUID.Scan(factID.String()); err != nil {
		return fmt.Errorf("%w: invalid fact id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsFactAuthor(ctx, db.IsFactAuthorParams{
		ID:       pgFactUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("check fact author: %w", err)
	}
	if !isAuthor {
		exists, err := s.querier.FactExists(ctx, pgFactUUID)
		if err != nil {
			return fmt.Errorf("check fact exists: %w", err)
		}
		if exists {
			return ErrForbidden
		}
		return ErrNotFound
	}

	existingCards, err := s.querier.GetCardsByFactID(ctx, pgFactUUID)
	if err != nil {
		return fmt.Errorf("get cards by fact id: %w", err)
	}

	if len(existingCards) != len(cardIDs) {
		return fmt.Errorf("%w: card list count mismatch", ErrValidation)
	}

	cardSet := make(map[uuid.UUID]bool, len(existingCards))
	for _, c := range existingCards {
		cardSet[c.ID.Bytes] = true
	}

	for _, cid := range cardIDs {
		if !cardSet[cid] {
			return fmt.Errorf("%w: card %s does not belong to fact %s", ErrValidation, cid, factID)
		}
	}

	for idx, cid := range cardIDs {
		pgCardUUID := pgtype.UUID{Bytes: cid, Valid: true}
		_, err := s.querier.UpdateCardStep(ctx, db.UpdateCardStepParams{
			ID:     pgCardUUID,
			Step:   int32(idx),
			FactID: pgFactUUID,
		})
		if err != nil {
			return fmt.Errorf("update card step: %w", err)
		}
	}

	return nil
}
