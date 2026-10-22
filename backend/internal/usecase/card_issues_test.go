package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestCardService_GetSetIssues(t *testing.T) {
	setID, cardID := uuid.New(), uuid.New()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	errDB := errors.New("connection reset")
	rows := []db.GetOpenCardIssuesBySetRow{{
		CardID:         pgtype.UUID{Bytes: cardID, Valid: true},
		Reason:         db.CardIssueReasonNotInNotes,
		Count:          3,
		LastReportedAt: pgtype.Timestamptz{Time: at, Valid: true},
	}}
	tests := []struct {
		name    string
		author  bool
		err     error
		want    []contracts.SetIssue
		wantErr error
	}{
		{"author", true, nil, []contracts.SetIssue{{CardId: cardID, Reason: contracts.CardIssueReasonNotInNotes, Count: 3, LastReportedAt: at}}, nil},
		{"not the author", false, nil, nil, usecase.ErrNotFound},
		{"database down", true, errDB, nil, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockQuerier{
				isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
					if uuid.UUID(arg.ID.Bytes) != setID || arg.AuthorID != 7 {
						t.Fatalf("author check = %+v", arg)
					}
					return tt.author, nil
				},
				getOpenCardIssuesBySetFunc: func(_ context.Context, arg db.GetOpenCardIssuesBySetParams) ([]db.GetOpenCardIssuesBySetRow, error) {
					if uuid.UUID(arg.SetID.Bytes) != setID || arg.UserID != 7 {
						t.Fatalf("params = %+v", arg)
					}
					return rows, tt.err
				},
			}
			svc := usecase.NewCardService(m, usecase.NewUserService(m))
			got, err := svc.GetSetIssues(context.Background(), 7, setID)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(got) != len(tt.want) || (len(got) == 1 && got[0] != tt.want[0]) {
				t.Errorf("issues = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCardService_ResolveCardIssues(t *testing.T) {
	cardID := uuid.New()
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		found   bool
		err     error
		wantErr error
	}{
		{"author", true, nil, nil},
		{"not the author or card gone", false, nil, usecase.ErrNotFound},
		{"database down", false, errDB, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockQuerier{resolveCardIssuesFunc: func(_ context.Context, arg db.ResolveCardIssuesParams) (bool, error) {
				if uuid.UUID(arg.CardID.Bytes) != cardID || arg.UserID != 7 {
					t.Fatalf("params = %+v", arg)
				}
				return tt.found, tt.err
			}}
			svc := usecase.NewCardService(m, usecase.NewUserService(m))
			err := svc.ResolveCardIssues(context.Background(), 7, cardID)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
