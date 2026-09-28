package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockUserQuerier struct {
	db.Querier
	upsertUserFunc func(ctx context.Context, arg db.UpsertUserParams) (db.User, error)
	ensureUserFunc func(ctx context.Context, id int64) (db.User, error)
}

func (m *mockUserQuerier) UpsertUser(ctx context.Context, arg db.UpsertUserParams) (db.User, error) {
	if m.upsertUserFunc != nil {
		return m.upsertUserFunc(ctx, arg)
	}
	return db.User{}, nil
}

func (m *mockUserQuerier) EnsureUser(ctx context.Context, id int64) (db.User, error) {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, id)
	}
	return db.User{}, nil
}

func TestUserService_UpsertUser(t *testing.T) {
	ctx := context.Background()
	lastName := "Smith"
	username := "jsmith"

	t.Run("successful upsert with all fields", func(t *testing.T) {
		var capturedArg db.UpsertUserParams
		mock := &mockUserQuerier{
			upsertUserFunc: func(_ context.Context, arg db.UpsertUserParams) (db.User, error) {
				capturedArg = arg
				return db.User{
					ID:        arg.ID,
					FirstName: arg.FirstName,
					LastName:  arg.LastName,
					Username:  arg.Username,
					IsBot:     arg.IsBot,
				}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.UpsertUser(ctx, usecase.UpsertUserParams{
			ID:        12345,
			FirstName: "John",
			LastName:  &lastName,
			Username:  &username,
			IsBot:     false,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedArg.ID != 12345 {
			t.Errorf("expected ID 12345, got %d", capturedArg.ID)
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
		var capturedArg db.UpsertUserParams
		mock := &mockUserQuerier{
			upsertUserFunc: func(_ context.Context, arg db.UpsertUserParams) (db.User, error) {
				capturedArg = arg
				return db.User{
					ID:        arg.ID,
					FirstName: arg.FirstName,
				}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.UpsertUser(ctx, usecase.UpsertUserParams{
			ID:        67890,
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
			upsertUserFunc: func(_ context.Context, _ db.UpsertUserParams) (db.User, error) {
				return db.User{}, mockErr
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.UpsertUser(ctx, usecase.UpsertUserParams{
			ID:        12345,
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
		var capturedID int64
		mock := &mockUserQuerier{
			ensureUserFunc: func(_ context.Context, id int64) (db.User, error) {
				capturedID = id
				return db.User{ID: id}, nil
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.EnsureUser(ctx, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedID != 42 {
			t.Errorf("expected ID 42, got %d", capturedID)
		}
	})

	t.Run("database error returns wrapped error", func(t *testing.T) {
		mockErr := errors.New("db error")
		mock := &mockUserQuerier{
			ensureUserFunc: func(_ context.Context, _ int64) (db.User, error) {
				return db.User{}, mockErr
			},
		}

		svc := usecase.NewUserService(mock)
		err := svc.EnsureUser(ctx, 42)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, mockErr) {
			t.Errorf("expected wrapped mockErr, got %v", err)
		}
	})
}
