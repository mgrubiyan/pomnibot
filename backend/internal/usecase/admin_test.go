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

func TestAdmins_Is(t *testing.T) {
	admins := usecase.Admins{MaxIDs: []int64{5}, TgIDs: []int64{6}}
	local := usecase.Admins{BrowserTestUser: true}
	tests := []struct {
		name    string
		admins  usecase.Admins
		account usecase.Account
		want    bool
	}{
		{"MAX admin", admins, usecase.Account{MaxID: 5}, true},
		{"Telegram admin", admins, usecase.Account{TgID: 6}, true},
		{"IDs of the other app", admins, usecase.Account{MaxID: 6, TgID: 5}, false},
		{"no chat account", admins, usecase.Account{}, false},
		{"browser test user locally", local, usecase.Account{}, true},
		{"chat user locally", local, usecase.Account{MaxID: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.admins.Is(tt.account); got != tt.want {
				t.Fatalf("Is = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminService_IsAdmin(t *testing.T) {
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		user    db.User
		err     error
		want    bool
		wantErr error
	}{
		{"admin", db.User{ID: 7, MaxID: pgtype.Int8{Int64: 5, Valid: true}}, nil, true, nil},
		{"not an admin", db.User{ID: 7, MaxID: pgtype.Int8{Int64: 9, Valid: true}}, nil, false, nil},
		{"no such user yet", db.User{}, pgx.ErrNoRows, false, nil},
		{"database down", db.User{}, errDB, false, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockQuerier{getUserByIDFunc: func(context.Context, int64) (db.User, error) { return tt.user, tt.err }}
			svc := usecase.NewAdminService(m, usecase.NewUserService(m), usecase.Admins{MaxIDs: []int64{5}})
			got, err := svc.IsAdmin(context.Background(), 7)
			if got != tt.want || !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("IsAdmin = %v, %v; want %v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestAdminService_ListCardIssues(t *testing.T) {
	id := uuid.New()
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	row := db.ListCardIssuesRow{
		ID:                pgtype.UUID{Bytes: id, Valid: true},
		Reason:            db.CardIssueReasonNotInNotes,
		Kind:              "input",
		Question:          "Q",
		AnswerText:        pgtype.Text{String: "A", Valid: true},
		Explanation:       "E",
		SourceQuote:       "Quote",
		CreatedAt:         pgtype.Timestamptz{Time: at, Valid: true},
		SetTitle:          pgtype.Text{String: "Биология", Valid: true},
		ReporterID:        22,
		ReporterFirstName: "Анна",
		ReporterLastName:  pgtype.Text{String: "Иванова", Valid: true},
		Status:            "fixed",
	}
	deleted := db.ListCardIssuesRow{ID: pgtype.UUID{Bytes: id, Valid: true}, Reason: db.CardIssueReasonOther, Kind: "table", Status: "deleted", ReporterID: 33, ReporterFirstName: "Иван"}
	want := []contracts.AdminCardIssue{
		{
			ID: id, Reason: contracts.CardIssueReasonNotInNotes, Status: contracts.CardIssueStatusFixed,
			SetTitle: contracts.NewOptString("Биология"), Kind: contracts.CardKindInput, Question: "Q",
			Answer: contracts.NewOptString("A"), Explanation: "E", SourceQuote: "Quote",
			Reporter:  contracts.User{ID: 22, FirstName: "Анна", LastName: contracts.NewOptString("Иванова")},
			CreatedAt: at,
		},
		{ID: id, Reason: contracts.CardIssueReasonOther, Status: contracts.CardIssueStatusDeleted, Kind: contracts.CardKindTable, Reporter: contracts.User{ID: 33, FirstName: "Иван"}},
	}
	errDB := errors.New("connection reset")
	tests := []struct {
		name    string
		maxID   int64
		listErr error
		want    []contracts.AdminCardIssue
		wantErr error
		listed  bool
	}{
		{"admin", 5, nil, want, nil, true},
		{"anyone else gets nothing", 9, nil, []contracts.AdminCardIssue{}, nil, false},
		{"database down", 5, errDB, nil, errDB, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed := false
			m := &mockQuerier{
				getUserByIDFunc: func(context.Context, int64) (db.User, error) {
					return db.User{ID: 7, MaxID: pgtype.Int8{Int64: tt.maxID, Valid: true}}, nil
				},
				listCardIssuesFunc: func(context.Context) ([]db.ListCardIssuesRow, error) {
					listed = true
					return []db.ListCardIssuesRow{row, deleted}, tt.listErr
				},
			}
			svc := usecase.NewAdminService(m, usecase.NewUserService(m), usecase.Admins{MaxIDs: []int64{5}})
			got, err := svc.ListCardIssues(context.Background(), 7)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if listed != tt.listed {
				t.Errorf("listed = %v, want %v", listed, tt.listed)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("issues = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
