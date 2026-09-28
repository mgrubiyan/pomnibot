package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type userServiceImpl struct {
	querier db.Querier
}

// NewUserService creates a new UserService implementation backed by db.Querier.
func NewUserService(querier db.Querier) UserService {
	return &userServiceImpl{querier: querier}
}

// UpsertUser upserts the user record with the provided parameters.
func (s *userServiceImpl) UpsertUser(ctx context.Context, params UpsertUserParams) error {
	var lastName pgtype.Text
	if params.LastName != nil {
		lastName = pgtype.Text{String: *params.LastName, Valid: true}
	}
	var username pgtype.Text
	if params.Username != nil {
		username = pgtype.Text{String: *params.Username, Valid: true}
	}

	_, err := s.querier.UpsertUser(ctx, db.UpsertUserParams{
		ID:        params.ID,
		FirstName: params.FirstName,
		LastName:  lastName,
		Username:  username,
		IsBot:     params.IsBot,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to upsert user in database", "userID", params.ID, "error", err)
		return fmt.Errorf("upsert user %d: %w", params.ID, err)
	}
	return nil
}

// EnsureUser ensures a user record exists in the database.
func (s *userServiceImpl) EnsureUser(ctx context.Context, userID int64) error {
	_, err := s.querier.EnsureUser(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to ensure user in database", "userID", userID, "error", err)
		return fmt.Errorf("ensure user %d: %w", userID, err)
	}
	return nil
}
