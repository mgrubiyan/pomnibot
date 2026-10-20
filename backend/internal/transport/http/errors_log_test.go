package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// captureLog sends the default logger to a buffer for the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestInternalErrorsAreLogged(t *testing.T) {
	logs := captureLog(t)
	h := NewSetsHandler(&mockSetService{
		getSetFunc: func(context.Context, int64, uuid.UUID) (*contracts.CardSet, error) {
			return nil, errors.New("connection refused")
		},
	})
	ctx := WithUserID(context.Background(), 42)

	res, err := h.GetSet(ctx, contracts.GetSetParams{SetId: uuid.New()})
	if err != nil {
		t.Fatalf("GetSet: %v", err)
	}
	if _, ok := res.(*contracts.GetSetInternalServerError); !ok {
		t.Fatalf("res = %T, want *contracts.GetSetInternalServerError", res)
	}

	got := logs.String()
	for _, want := range []string{"level=ERROR", "operation=GetSet", "user_id=42", "connection refused"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q does not contain %q", got, want)
		}
	}
}

func TestNotFoundIsNotLoggedAsError(t *testing.T) {
	logs := captureLog(t)
	h := NewSetsHandler(&mockSetService{
		getSetFunc: func(context.Context, int64, uuid.UUID) (*contracts.CardSet, error) {
			return nil, usecase.ErrNotFound
		},
	})

	if _, err := h.GetSet(context.Background(), contracts.GetSetParams{SetId: uuid.New()}); err != nil {
		t.Fatalf("GetSet: %v", err)
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("a missing set was logged as an error: %q", logs.String())
	}
}
