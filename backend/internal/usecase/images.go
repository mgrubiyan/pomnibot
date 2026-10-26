package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// ImageService serves only images of sets the caller can access.
type ImageService interface {
	GetSetImages(context.Context, uuid.UUID, uuid.UUID) ([]contracts.CardImage, error)
	GetCardImage(context.Context, uuid.UUID, uuid.UUID) ([]byte, error)
}

type imageService struct{ q db.Querier }

// NewImageService shares the membership rules used for sets.
func NewImageService(q db.Querier) ImageService { return &imageService{q} }
func (s *imageService) GetSetImages(ctx context.Context, userID, setID uuid.UUID) ([]contracts.CardImage, error) {
	id := pgtype.UUID{Bytes: setID, Valid: true}
	if _, err := s.q.GetSetByID(ctx, db.GetSetByIDParams{ID: id, UserID: pgtype.UUID{Bytes: userID, Valid: true}}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.q.GetSetImages(ctx, id)
	if err != nil {
		return nil, err
	}
	images := make([]contracts.CardImage, 0, len(rows))
	for _, r := range rows {
		images = append(images, imageContract(r))
	}
	return images, nil
}
func (s *imageService) GetCardImage(ctx context.Context, userID, assetID uuid.UUID) ([]byte, error) {
	data, err := s.q.GetImageForUser(ctx, db.GetImageForUserParams{ID: pgtype.UUID{Bytes: assetID, Valid: true}, UserID: pgtype.UUID{Bytes: userID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}
func imageContract(r db.GetSetImagesRow) contracts.CardImage {
	id := uuid.UUID(r.ID.Bytes)
	return contracts.CardImage{AssetId: id, URL: "/api/images/" + id.String(), Alt: r.Alt, Width: int(r.Width), Height: int(r.Height), SourcePage: int(r.SourcePage), SourceName: r.SourceName}
}
func loadImages(ctx context.Context, q db.Querier, id pgtype.UUID) ([]contracts.CardImage, []contracts.CardImage, error) {
	rows, err := q.GetCardImages(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("load card images: %w", err)
	}
	question, explanation := []contracts.CardImage{}, []contracts.CardImage{}
	for _, r := range rows {
		img := imageContract(db.GetSetImagesRow{ID: r.ID, SetID: r.SetID, Alt: r.Alt, Width: r.Width, Height: r.Height, SourceName: r.SourceName, SourcePage: r.SourcePage})
		if r.Side == "question" {
			question = append(question, img)
		} else {
			explanation = append(explanation, img)
		}
	}
	return question, explanation, nil
}
func setResponseImages(ctx context.Context, q db.Querier, id pgtype.UUID, c *contracts.Card) error {
	var err error
	c.QuestionImages, c.ExplanationImages, err = loadImages(ctx, q, id)
	return err
}
