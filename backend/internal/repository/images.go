package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// ErrInvalidImage means an attachment is duplicate or outside this set.
var ErrInvalidImage = errors.New("invalid image attachment")

// ReplaceCardImages preserves omitted sides and clears explicitly empty sides.
// The caller owns the transaction, so validation and both sides commit together.
func ReplaceCardImages(ctx context.Context, q db.Querier, cardID, setID pgtype.UUID, question, explanation []uuid.UUID) error {
	if question == nil && explanation == nil {
		return nil
	}
	assets, err := q.GetSetImages(ctx, setID)
	if err != nil {
		return err
	}
	known := map[uuid.UUID]bool{}
	for _, a := range assets {
		known[uuid.UUID(a.ID.Bytes)] = true
	}
	for _, ids := range [][]uuid.UUID{question, explanation} {
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			if !known[id] || seen[id] {
				return ErrInvalidImage
			}
			seen[id] = true
		}
	}
	for _, side := range []struct {
		name string
		ids  []uuid.UUID
	}{{"question", question}, {"explanation", explanation}} {
		if side.ids == nil {
			continue
		}
		if err := q.DeleteCardImages(ctx, db.DeleteCardImagesParams{CardID: cardID, Side: side.name}); err != nil {
			return err
		}
		for pos, id := range side.ids {
			count, err := q.AttachCardImage(ctx, db.AttachCardImageParams{CardID: cardID, AssetID: pgtype.UUID{Bytes: id, Valid: true}, Side: side.name, Position: int32(pos)})
			if err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("%w: image set mismatch", ErrInvalidImage)
			}
		}
	}
	return nil
}
