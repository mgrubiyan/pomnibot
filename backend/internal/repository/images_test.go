package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"testing"
)

type imageQueries struct {
	db.Querier
	images   []db.GetSetImagesRow
	deleted  []string
	attached []db.AttachCardImageParams
}

func (q *imageQueries) GetSetImages(context.Context, pgtype.UUID) ([]db.GetSetImagesRow, error) {
	return q.images, nil
}
func (q *imageQueries) DeleteCardImages(_ context.Context, p db.DeleteCardImagesParams) error {
	q.deleted = append(q.deleted, p.Side)
	return nil
}
func (q *imageQueries) AttachCardImage(_ context.Context, p db.AttachCardImageParams) (int64, error) {
	q.attached = append(q.attached, p)
	return 1, nil
}
func TestReplaceCardImages(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	q := &imageQueries{images: []db.GetSetImagesRow{{ID: pgtype.UUID{Bytes: a, Valid: true}}, {ID: pgtype.UUID{Bytes: b, Valid: true}}}}
	if err := ReplaceCardImages(t.Context(), q, pgtype.UUID{}, pgtype.UUID{}, []uuid.UUID{b, a}, []uuid.UUID{}); err != nil {
		t.Fatal(err)
	}
	if len(q.deleted) != 2 || len(q.attached) != 2 || q.attached[0].AssetID.Bytes != b || q.attached[1].Position != 1 {
		t.Fatalf("order lost: %+v", q)
	}
	q.deleted = nil
	q.attached = nil
	if err := ReplaceCardImages(t.Context(), q, pgtype.UUID{}, pgtype.UUID{}, nil, nil); err != nil || len(q.deleted) != 0 {
		t.Fatal("omitted did not preserve", err)
	}
	if err := ReplaceCardImages(t.Context(), q, pgtype.UUID{}, pgtype.UUID{}, []uuid.UUID{uuid.New()}, nil); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("foreign image accepted: %v", err)
	}
	if len(q.deleted) != 0 {
		t.Fatal("invalid update mutated links")
	}
	if err := ReplaceCardImages(t.Context(), q, pgtype.UUID{}, pgtype.UUID{}, []uuid.UUID{a, a}, nil); !errors.Is(err, ErrInvalidImage) {
		t.Fatal("duplicate accepted")
	}
}
