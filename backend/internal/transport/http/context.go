package http

import "context"

type contextKey string

const (
	userIDContextKey   contextKey = "userID"
	userNameContextKey contextKey = "userName"
)

// WithUserID injects the given user ID into the context.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext retrieves the user ID from the context if present.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	val, ok := ctx.Value(userIDContextKey).(int64)
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
