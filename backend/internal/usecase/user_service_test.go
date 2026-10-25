package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

var (
	testUserUUID1 = uuid.MustParse("00000000-0000-0000-0000-000000000123")
	testUserUUID2 = uuid.MustParse("00000000-0000-0000-0000-000000000456")
	testUserUUID3 = uuid.MustParse("00000000-0000-0000-0000-000000000789")
)

type mockUserQuerier struct {
	db.Querier
	upsertMAXUserFunc        func(ctx context.Context, arg db.UpsertMAXUserParams) (db.User, error)
	ensureUserFunc           func(ctx context.Context, id pgtype.UUID) (db.User, error)
	getUsersWithDueFactsFunc func(ctx context.Context, arg db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error)
	setDataConsentFunc       func(ctx context.Context, arg db.SetDataConsentParams) (db.User, error)
}

func (m *mockUserQuerier) SetDataConsent(ctx context.Context, arg db.SetDataConsentParams) (db.User, error) {
	return m.setDataConsentFunc(ctx, arg)
}

func (m *mockUserQuerier) UpsertMAXUser(ctx context.Context, arg db.UpsertMAXUserParams) (db.User, error) {
	if m.upsertMAXUserFunc != nil {
		return m.upsertMAXUserFunc(ctx, arg)
	}
	return db.User{}, nil
}

func (m *mockUserQuerier) EnsureUser(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, id)
	}
	return db.User{}, nil
}

func (m *mockUserQuerier) GetUsersWithDueFacts(ctx context.Context, arg db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error) {
	if m.getUsersWithDueFactsFunc != nil {
		return m.getUsersWithDueFactsFunc(ctx, arg)
	}
	return nil, nil
}

func TestUserService_UpsertMAXUser(t *testing.T) {
	ctx := context.Background()
	lastName := "Smith"
	username := "jsmith"

	t.Run("successful upsert with all fields", func(t *testing.T) {
		var capturedArg db.UpsertMAXUserParams
		mock := &mockUserQuerier{
			upsertMAXUserFunc: func(_ context.Context, arg db.UpsertMAXUserParams) (db.User, error) {
				capturedArg = arg
				return db.User{
					ID:        pgtype.UUID{Bytes: testUserUUID1, Valid: true},
					MaxID:     arg.MaxID,
					FirstName: arg.FirstName,
					LastName:  arg.LastName,
					Username:  arg.Username,
					IsBot:     arg.IsBot,
				}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		_, err := svc.UpsertMAXUser(ctx, usecase.MAXUserParams{
			MaxID:     12345,
			FirstName: "John",
			LastName:  &lastName,
			Username:  &username,
			IsBot:     false,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedArg.MaxID.Int64 != 12345 {
			t.Errorf("expected ID 12345, got %d", capturedArg.MaxID.Int64)
		}
		if capturedArg.FirstName != "John" {
			t.Errorf("expected FirstName 'John', got '%s'", capturedArg.FirstName)
		}
		if !capturedArg.LastName.Valid || capturedArg.LastName.String != "Smith" {
			t.Errorf("expected LastName 'Smith', got %+v", capturedArg.LastName)
		}
		if !capturedArg.Username.Valid || capturedArg.Username.String != "jsmith" {
			t.Errorf("expected Username 'jsmith', got %+v", capturedArg.Username)
		}
		if capturedArg.IsBot {
			t.Errorf("expected IsBot false, got true")
		}
	})

	t.Run("successful upsert with nil optional fields", func(t *testing.T) {
		var capturedArg db.UpsertMAXUserParams
		mock := &mockUserQuerier{
			upsertMAXUserFunc: func(_ context.Context, arg db.UpsertMAXUserParams) (db.User, error) {
				capturedArg = arg
				return db.User{
					ID:        pgtype.UUID{Bytes: testUserUUID1, Valid: true},
					MaxID:     arg.MaxID,
					FirstName: arg.FirstName,
				}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		_, err := svc.UpsertMAXUser(ctx, usecase.MAXUserParams{
			MaxID:     67890,
			FirstName: "Alice",
			LastName:  nil,
			Username:  nil,
			IsBot:     true,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedArg.LastName.Valid {
			t.Errorf("expected LastName to be invalid/null, got valid: %v", capturedArg.LastName)
		}
		if capturedArg.Username.Valid {
			t.Errorf("expected Username to be invalid/null, got valid: %v", capturedArg.Username)
		}
		if !capturedArg.IsBot {
			t.Errorf("expected IsBot true, got false")
		}
	})

	t.Run("database error returns wrapped error", func(t *testing.T) {
		mockErr := errors.New("connection failed")
		mock := &mockUserQuerier{
			upsertMAXUserFunc: func(_ context.Context, _ db.UpsertMAXUserParams) (db.User, error) {
				return db.User{}, mockErr
			},
		}

		svc := usecase.NewUserService(mock)
		_, err := svc.UpsertMAXUser(ctx, usecase.MAXUserParams{
			MaxID:     12345,
			FirstName: "John",
		})

		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, mockErr) {
			t.Errorf("expected wrapped mockErr, got %v", err)
		}
	})
}

func TestUserService_EnsureUser(t *testing.T) {
	ctx := context.Background()

	t.Run("successful ensure user", func(t *testing.T) {
		var capturedID pgtype.UUID
		mock := &mockUserQuerier{
			ensureUserFunc: func(_ context.Context, id pgtype.UUID) (db.User, error) {
				capturedID = id
				return db.User{ID: id}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.EnsureUser(ctx, testUserUUID1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedID.Bytes != testUserUUID1 {
			t.Errorf("expected ID %s, got %s", testUserUUID1, capturedID.Bytes)
		}
	})

	t.Run("database error returns wrapped error", func(t *testing.T) {
		mockErr := errors.New("db error")
		mock := &mockUserQuerier{
			ensureUserFunc: func(_ context.Context, _ pgtype.UUID) (db.User, error) {
				return db.User{}, mockErr
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.EnsureUser(ctx, testUserUUID1)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, mockErr) {
			t.Errorf("expected wrapped mockErr, got %v", err)
		}
	})
}

func TestUserService_GetUsersWithDueFacts(t *testing.T) {
	ctx := context.Background()
	fixedTime := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

	t.Run("successful retrieval with default timezone", func(t *testing.T) {
		var capturedArg db.GetUsersWithDueFactsParams
		mock := &mockUserQuerier{
			getUsersWithDueFactsFunc: func(_ context.Context, arg db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error) {
				capturedArg = arg
				return []pgtype.UUID{{Bytes: testUserUUID1, Valid: true}, {Bytes: testUserUUID2, Valid: true}}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		users, err := svc.GetUsersWithDueFacts(ctx, fixedTime, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if capturedArg.Tz != "Europe/Moscow" {
			t.Errorf("expected default timezone 'Europe/Moscow', got '%s'", capturedArg.Tz)
		}
		if !capturedArg.Now.Valid || capturedArg.Now.Time != fixedTime {
			t.Errorf("expected Now timestamp %+v, got %+v", fixedTime, capturedArg.Now.Time)
		}
		if len(users) != 2 || users[0] != testUserUUID1 || users[1] != testUserUUID2 {
			t.Errorf("expected [%s, %s], got %v", testUserUUID1, testUserUUID2, users)
		}
	})

	t.Run("filters out bypass nil UUID", func(t *testing.T) {
		mock := &mockUserQuerier{
			getUsersWithDueFactsFunc: func(_ context.Context, _ db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error) {
				return []pgtype.UUID{
					{Bytes: uuid.Nil, Valid: true},
					{Valid: false},
					{Bytes: testUserUUID3, Valid: true},
				}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		users, err := svc.GetUsersWithDueFacts(ctx, fixedTime, "Europe/Moscow")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 1 || users[0] != testUserUUID3 {
			t.Errorf("expected only [%s], got %v", testUserUUID3, users)
		}
	})

	t.Run("database error returns wrapped error", func(t *testing.T) {
		mockErr := errors.New("query failed")
		mock := &mockUserQuerier{
			getUsersWithDueFactsFunc: func(_ context.Context, _ db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error) {
				return nil, mockErr
			},
		}

		svc := usecase.NewUserService(mock)
		users, err := svc.GetUsersWithDueFacts(ctx, fixedTime, "Europe/Moscow")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, mockErr) {
			t.Errorf("expected wrapped mockErr, got %v", err)
		}
		if users != nil {
			t.Errorf("expected nil users on error, got %v", users)
		}
	})
}

func TestUserService_SetDataConsent(t *testing.T) {
	var got db.SetDataConsentParams
	mock := &mockUserQuerier{setDataConsentFunc: func(_ context.Context, arg db.SetDataConsentParams) (db.User, error) {
		got = arg
		row := db.User{ID: arg.ID, FirstName: "Ann"}
		if arg.Given {
			row.DataConsentAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
		return row, nil
	}}
	svc := usecase.NewUserService(mock)

	account, err := svc.SetDataConsent(context.Background(), testUserUUID1, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != (pgtype.UUID{Bytes: testUserUUID1, Valid: true}) || !got.Given {
		t.Fatalf("query params = %+v", got)
	}
	if !account.DataConsent || account.User.ID != testUserUUID1 {
		t.Fatalf("account after consent = %+v", account)
	}

	account, err = svc.SetDataConsent(context.Background(), testUserUUID1, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Given || account.DataConsent {
		t.Fatalf("withdrawn consent kept: params %+v, account %+v", got, account)
	}
}
