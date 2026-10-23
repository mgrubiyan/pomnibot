package http

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	userIDContextKey   contextKey = "userID"
	userNameContextKey contextKey = "userName"
)

// WithUserID injects the given user ID into the context.
func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext retrieves the user ID from the context if present.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	val, ok := ctx.Value(userIDContextKey).(uuid.UUID)
	return val, ok
}

// WithUserName injects the given user name into the context.
func WithUserName(ctx context.Context, userName string) context.Context {
	return context.WithValue(ctx, userNameContextKey, userName)
}

// UserNameFromContext retrieves the user name from the context if present.
func UserNameFromContext(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(userNameContextKey).(string)
	return val, ok
}
