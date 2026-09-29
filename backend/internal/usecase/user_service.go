package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// GetUsersWithDueFacts returns all user IDs who have cards due today or earlier in the given timezone.
func (s *userServiceImpl) GetUsersWithDueFacts(ctx context.Context, now time.Time, tz string) ([]int64, error) {
	if strings.TrimSpace(tz) == "" {
		tz = "Europe/Moscow"
	}

	pgNow := pgtype.Timestamptz{
		Time:  now,
		Valid: true,
	}

	rawUsers, err := s.querier.GetUsersWithDueFacts(ctx, db.GetUsersWithDueFactsParams{
		Tz:  tz,
		Now: pgNow,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to get users with due facts from database", "error", err)
		return nil, fmt.Errorf("get users with due facts: %w", err)
	}

	users := make([]int64, 0, len(rawUsers))
	for _, id := range rawUsers {
		if id == 0 || id == 100001 {
			continue
		}
		users = append(users, id)
	}

	return users, nil
}
