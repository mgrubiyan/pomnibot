package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// EnsureUser ensures that a record for userID exists in the users table.
// If userID is 0, default user name "Browser Test User" is used.
// If name is not provided, "MAX User" is used.
func EnsureUser(ctx context.Context, querier db.Querier, userID int64, defaultName string) error {
	name := defaultName
	if userID == 0 {
		name = "Browser Test User"
	} else if name == "" {
		name = fmt.Sprintf("MAX User %d", userID)
	}

	_, err := querier.UpsertUser(ctx, db.UpsertUserParams{
		ID:   userID,
		Name: name,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to ensure user in database", "userID", userID, "error", err)
		return fmt.Errorf("ensure user %d: %w", userID, err)
	}
	return nil
}
