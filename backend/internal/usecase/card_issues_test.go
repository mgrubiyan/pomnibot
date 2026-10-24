package usecase_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestCardService_ReportCardIssue(t *testing.T) {
	cardID := uuid.New()
	userUUID := uuid.New()
	authorUUID := uuid.New()
	pgAuthorUUID := pgtype.UUID{Bytes: authorUUID, Valid: true}
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		row     db.CreateCardIssueRow
		err     error
		want    []usecase.CardIssueReport
		wantErr error
	}{
		{"first complaint", db.CreateCardIssueRow{AuthorID: pgAuthorUUID, SetTitle: "Биология", NotifyAuthor: true}, nil,
			[]usecase.CardIssueReport{{AuthorID: authorUUID, SetTitle: "Биология"}}, nil},
		{"author already told", db.CreateCardIssueRow{AuthorID: pgAuthorUUID, SetTitle: "Биология"}, nil, nil, nil},
		{"card gone or not visible", db.CreateCardIssueRow{}, pgx.ErrNoRows, nil, usecase.ErrNotFound},
		{"database down", db.CreateCardIssueRow{}, errDB, nil, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got db.CreateCardIssueParams
			m := &mockQuerier{createCardIssueFunc: func(_ context.Context, arg db.CreateCardIssueParams) (db.CreateCardIssueRow, error) {
				got = arg
				return tt.row, tt.err
			}}
			notifier := &issueNotifierSpy{}
			svc := usecase.NewCardService(m, usecase.NewUserService(m), usecase.WithIssueNotifier(notifier))
			err := svc.ReportCardIssue(context.Background(), userUUID, cardID, contracts.CardIssueReasonWording)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if uuid.UUID(got.UserID.Bytes) != userUUID || uuid.UUID(got.CardID.Bytes) != cardID || !got.CardID.Valid || got.Reason != db.CardIssueReasonWording {
				t.Errorf("params = %+v", got)
			}
			if !reflect.DeepEqual(notifier.reports, tt.want) {
				t.Errorf("reports = %+v, want %+v", notifier.reports, tt.want)
			}
		})
	}
}

// Without a notifier, as locally, the complaint is still saved.
func TestCardService_ReportCardIssue_NoNotifier(t *testing.T) {
	authorUUID := uuid.New()
	m := &mockQuerier{createCardIssueFunc: func(context.Context, db.CreateCardIssueParams) (db.CreateCardIssueRow, error) {
		return db.CreateCardIssueRow{AuthorID: pgtype.UUID{Bytes: authorUUID, Valid: true}, NotifyAuthor: true}, nil
	}}
	svc := usecase.NewCardService(m, usecase.NewUserService(m))
	if err := svc.ReportCardIssue(context.Background(), uuid.New(), uuid.New(), contracts.CardIssueReasonOther); err != nil {
		t.Fatal(err)
	}
}

type issueNotifierSpy struct{ reports []usecase.CardIssueReport }

func (s *issueNotifierSpy) CardIssueReported(_ context.Context, r usecase.CardIssueReport) {
	s.reports = append(s.reports, r)
}

func TestCardService_GetSetIssues(t *testing.T) {
	setID, cardID := uuid.New(), uuid.New()
	userUUID := uuid.New()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	errDB := errors.New("connection reset")
	rows := []db.GetSetIssuesRow{{
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
					if uuid.UUID(arg.ID.Bytes) != setID || uuid.UUID(arg.AuthorID.Bytes) != userUUID {
						t.Fatalf("author check = %+v", arg)
					}
					return tt.author, nil
				},
				getSetIssuesFunc: func(_ context.Context, arg db.GetSetIssuesParams) ([]db.GetSetIssuesRow, error) {
					if uuid.UUID(arg.SetID.Bytes) != setID || uuid.UUID(arg.UserID.Bytes) != userUUID {
						t.Fatalf("params = %+v", arg)
					}
					return rows, tt.err
				},
			}
			svc := usecase.NewCardService(m, usecase.NewUserService(m))
			got, err := svc.GetSetIssues(context.Background(), userUUID, setID)
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
	userUUID := uuid.New()
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
			m := &mockQuerier{closeCardIssuesFunc: func(_ context.Context, arg db.CloseCardIssuesParams) (bool, error) {
				if uuid.UUID(arg.CardID.Bytes) != cardID || uuid.UUID(arg.UserID.Bytes) != userUUID {
					t.Fatalf("params = %+v", arg)
				}
				return tt.found, tt.err
			}}
			svc := usecase.NewCardService(m, usecase.NewUserService(m))
			err := svc.ResolveCardIssues(context.Background(), userUUID, cardID)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
