package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

var testMaxUUID = uuid.MustParse("00000000-0000-0000-0000-000000000900")

type platformUserQuerier struct {
	db.Querier
	failure error
}

func (q platformUserQuerier) UpsertMAXUser(_ context.Context, p db.UpsertMAXUserParams) (db.User, error) {
	return db.User{ID: pgtype.UUID{Bytes: testMaxUUID, Valid: true}, MaxID: p.MaxID, FirstName: p.FirstName}, q.failure
}

func TestPlatformUsersReturnInternalIDs(t *testing.T) {
	ctx := context.Background()
	users := usecase.NewUserService(platformUserQuerier{})
	maxUser, err := users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 42, FirstName: "MAX"})
	if err != nil || maxUser.User.ID != testMaxUUID || maxUser.MaxID != 42 {
		t.Fatalf("MAX account: %+v %v", maxUser, err)
	}
}

func TestPlatformUsersRejectInvalidExternalIDs(t *testing.T) {
	users := usecase.NewUserService(nil)
	for _, id := range []int64{0, -1} {
		if _, err := users.UpsertMAXUser(context.Background(), usecase.MAXUserParams{MaxID: id}); !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("MAX ID=%d: %v", id, err)
		}
	}
}

func TestPlatformUsersPreserveDatabaseErrors(t *testing.T) {
	failure := errors.New("database unavailable")
	users := usecase.NewUserService(platformUserQuerier{failure: failure})
	if _, err := users.UpsertMAXUser(context.Background(), usecase.MAXUserParams{MaxID: 42}); !errors.Is(err, failure) {
		t.Fatalf("MAX: %v", err)
	}
}
