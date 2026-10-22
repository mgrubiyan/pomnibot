package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestCardService_ReportCardIssue(t *testing.T) {
	cardID := uuid.New()
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		rows    int64
		err     error
		wantErr error
	}{
		{"saved", 1, nil, nil},
		{"card gone or not visible", 0, nil, usecase.ErrNotFound},
		{"database down", 0, errDB, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got db.CreateCardIssueParams
			m := &mockQuerier{createCardIssueFunc: func(_ context.Context, arg db.CreateCardIssueParams) (int64, error) {
				got = arg
				return tt.rows, tt.err
			}}
			svc := usecase.NewCardService(m, usecase.NewUserService(m))
			err := svc.ReportCardIssue(context.Background(), 7, cardID, contracts.CardIssueReasonWording)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.UserID != 7 || uuid.UUID(got.CardID.Bytes) != cardID || !got.CardID.Valid || got.Reason != db.CardIssueReasonWording {
				t.Errorf("params = %+v", got)
			}
		})
	}
}
