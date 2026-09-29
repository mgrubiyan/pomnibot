package http

import (
	"context"
	"testing"
)

func TestContextUserID(t *testing.T) {
	t.Run("with user id", func(t *testing.T) {
		ctx := WithUserID(context.Background(), 12345)
		id, ok := UserIDFromContext(ctx)
		if !ok {
			t.Fatal("expected UserIDFromContext to return true, got false")
		}
		if id != 12345 {
			t.Fatalf("expected user id 12345, got %d", id)
		}
	})

	t.Run("empty context", func(t *testing.T) {
		id, ok := UserIDFromContext(context.Background())
		if ok {
			t.Fatalf("expected UserIDFromContext to return false for empty context, got true with id %d", id)
		}
		if id != 0 {
			t.Fatalf("expected default 0, got %d", id)
		}
	})
}
