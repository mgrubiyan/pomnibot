package http

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestContextUserID(t *testing.T) {
	testID := uuid.MustParse("00000000-0000-0000-0000-000000012345")

	t.Run("with user id", func(t *testing.T) {
		ctx := WithUserID(context.Background(), testID)
		id, ok := UserIDFromContext(ctx)
		if !ok {
			t.Fatal("expected UserIDFromContext to return true, got false")
		}
		if id != testID {
			t.Fatalf("expected user id %s, got %s", testID, id)
		}
	})

	t.Run("empty context", func(t *testing.T) {
		id, ok := UserIDFromContext(context.Background())
		if ok {
			t.Fatalf("expected UserIDFromContext to return false for empty context, got true with id %s", id)
		}
		if id != uuid.Nil {
			t.Fatalf("expected default %s, got %s", uuid.Nil, id)
		}
	})
}
