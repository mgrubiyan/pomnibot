package http

import "context"

type contextKey string

const userIDContextKey contextKey = "userID"

// WithUserID injects the given user ID into the context.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext retrieves the user ID from the context if present.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	val, ok := ctx.Value(userIDContextKey).(int64)
	return val, ok
}
