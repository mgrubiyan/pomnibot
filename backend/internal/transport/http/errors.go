package http

import (
	"context"
	"log/slog"
)

// internalError logs an error the client only sees as a 500, so that the
// cause is in the server log, and returns the message for the response.
func internalError(ctx context.Context, op string, err error) string {
	userID, _ := UserIDFromContext(ctx)
	slog.ErrorContext(ctx, "request failed", "operation", op, "user_id", userID, "error", err)
	return err.Error()
}
