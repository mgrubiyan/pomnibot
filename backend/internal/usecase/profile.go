package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// SetDataConsent gives or withdraws the user's consent to show set authors
// their name in the leaderboard and the mistakes of a set.
func (s *userServiceImpl) SetDataConsent(ctx context.Context, id uuid.UUID, given bool) (Account, error) {
	row, err := s.querier.SetDataConsent(ctx, db.SetDataConsentParams{ID: pgtype.UUID{Bytes: id, Valid: true}, Given: given})
	if err != nil {
		return Account{}, fmt.Errorf("set data consent: %w", err)
	}
	return accountFromUser(row), nil
}
