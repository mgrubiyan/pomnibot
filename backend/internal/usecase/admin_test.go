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
	admins := usecase.Admins{MaxIDs: []int64{5}}
	local := usecase.Admins{BrowserTestUser: true}
	tests := []struct {
		name    string
		admins  usecase.Admins
		account usecase.Account
		want    bool
	}{
		{"MAX admin", admins, usecase.Account{MaxID: 5}, true},
		{"another MAX user", admins, usecase.Account{MaxID: 6}, false},
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
	adminUUID := uuid.New()
	pgAdminUUID := pgtype.UUID{Bytes: adminUUID, Valid: true}
	tests := []struct {
		name    string
		user    db.User
		err     error
		want    bool
		wantErr error
	}{
		{"admin", db.User{ID: pgAdminUUID, MaxID: pgtype.Int8{Int64: 5, Valid: true}}, nil, true, nil},
		{"not an admin", db.User{ID: pgAdminUUID, MaxID: pgtype.Int8{Int64: 9, Valid: true}}, nil, false, nil},
		{"no such user yet", db.User{}, pgx.ErrNoRows, false, nil},
		{"database down", db.User{}, errDB, false, errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockQuerier{getUserByIDFunc: func(context.Context, pgtype.UUID) (db.User, error) { return tt.user, tt.err }}
			svc := usecase.NewAdminService(m, usecase.NewUserService(m), usecase.Admins{MaxIDs: []int64{5}})
			got, err := svc.IsAdmin(context.Background(), adminUUID)
			if got != tt.want || !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("IsAdmin = %v, %v; want %v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestAdminService_ListCardIssues(t *testing.T) {
	id := uuid.New()
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	adminUUID := uuid.New()
	pgAdminUUID := pgtype.UUID{Bytes: adminUUID, Valid: true}
	reporterID1 := uuid.New()
	reporterID2 := uuid.New()
	row := db.ListCardIssuesRow{
		ID:                pgtype.UUID{Bytes: id, Valid: true},
		Reason:            db.CardIssueReasonNotInNotes,
		Kind:              db.CardKindInput,
		Question:          "Q",
		AnswerText:        pgtype.Text{String: "A", Valid: true},
		Explanation:       "E",
		SourceQuote:       "Quote",
		CreatedAt:         pgtype.Timestamptz{Time: at, Valid: true},
		SetTitle:          pgtype.Text{String: "Биология", Valid: true},
		ReporterID:        pgtype.UUID{Bytes: reporterID1, Valid: true},
		ReporterFirstName: "Анна",
		ReporterLastName:  pgtype.Text{String: "Иванова", Valid: true},
		Status:            "fixed",
	}
	deleted := db.ListCardIssuesRow{
		ID:                pgtype.UUID{Bytes: id, Valid: true},
		Reason:            db.CardIssueReasonOther,
		Comment:           pgtype.Text{String: "Таблица перепутана", Valid: true},
		Kind:              db.CardKindTable,
		Status:            "deleted",
		ReporterID:        pgtype.UUID{Bytes: reporterID2, Valid: true},
		ReporterFirstName: "Иван",
	}
	want := []contracts.AdminCardIssue{
		{
			ID: id, Reason: contracts.CardIssueReasonNotInNotes, Status: contracts.CardIssueStatusFixed,
			SetTitle: contracts.NewOptString("Биология"), Kind: contracts.CardKindInput, Question: "Q",
			Answer: contracts.NewOptString("A"), Explanation: "E", SourceQuote: "Quote",
			Reporter:  contracts.User{ID: reporterID1, FirstName: "Анна", LastName: contracts.NewOptString("Иванова")},
			CreatedAt: at,
		},
		{ID: id, Reason: contracts.CardIssueReasonOther, Comment: contracts.NewOptString("Таблица перепутана"), Status: contracts.CardIssueStatusDeleted, Kind: contracts.CardKindTable, Reporter: contracts.User{ID: reporterID2, FirstName: "Иван"}},
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
				getUserByIDFunc: func(context.Context, pgtype.UUID) (db.User, error) {
					return db.User{ID: pgAdminUUID, MaxID: pgtype.Int8{Int64: tt.maxID, Valid: true}}, nil
				},
				listCardIssuesFunc: func(context.Context) ([]db.ListCardIssuesRow, error) {
					listed = true
					return []db.ListCardIssuesRow{row, deleted}, tt.listErr
				},
			}
			svc := usecase.NewAdminService(m, usecase.NewUserService(m), usecase.Admins{MaxIDs: []int64{5}})
			got, err := svc.ListCardIssues(context.Background(), adminUUID)
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
