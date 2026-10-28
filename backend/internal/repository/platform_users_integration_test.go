//go:build integration

package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestPlatformUsersMigrationAndSharedLearning(t *testing.T) {
	ctx := context.Background()
	oldSet := uuid.New()
	pool := manualTestPool(t, func(pool *pgxpool.Pool) {
		for _, sql := range []string{
			"INSERT INTO users(id,first_name) VALUES(0,'Test'),(1,'Demo'),(100001,'Demo'),(9999999,'Existing MAX')",
			"INSERT INTO sets(id,title,author_id,share_code) VALUES('" + oldSet.String() + "','Existing',9999999,'111111')",
			"INSERT INTO user_sets(user_id,set_id) VALUES(9999999,'" + oldSet.String() + "')",
			"INSERT INTO facts(id,set_id,name) VALUES('legacy-fact','" + oldSet.String() + "','Legacy')",
			"INSERT INTO user_fact_progress(user_id,fact_id,elo) VALUES(9999999,'legacy-fact',900)",
		} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
	})
	q := db.New(pool)
	users := usecase.NewUserService(q)
	maxAccount, err := users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 9999999, FirstName: "Updated MAX"})
	if err != nil {
		t.Fatal(err)
	}
	if maxAccount.MaxID != 9999999 {
		t.Fatalf("legacy ID changed: %d", maxAccount.MaxID)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = '00000000-0000-0000-0000-000000000000' AND max_id IS NOT NULL").Scan(&count); err != nil || count != 0 {
		t.Fatalf("demo platform IDs: %d %v", count, err)
	}
	var step int
	if err = pool.QueryRow(ctx, "SELECT step FROM user_fact_progress WHERE user_id=$1", maxAccount.User.ID).Scan(&step); err != nil || step != 1 {
		t.Fatalf("progress step: %d %v", step, err)
	}
	var owner uuid.UUID
	if err = pool.QueryRow(ctx, "SELECT author_id FROM sets WHERE share_code='111111'").Scan(&owner); err != nil || owner != maxAccount.User.ID {
		t.Fatalf("owner: %s %v", owner, err)
	}
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 32)
	failures := make(chan error, 32)
	for range 32 {
		wg.Go(func() {
			account, err := users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 8888888, FirstName: "Member"})
			if err != nil {
				failures <- err
				return
			}
			ids <- account.User.ID
		})
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var memberID uuid.UUID
	for id := range ids {
		if memberID == uuid.Nil {
			memberID = id
		}
		if id != memberID {
			t.Fatal("duplicate accounts")
		}
	}
	if memberID == maxAccount.User.ID {
		t.Fatalf("accounts clashed: %s", memberID)
	}
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	cards := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	set, err := sets.CreateSet(ctx, maxAccount.User.ID, &contracts.CreateSetRequest{Title: "Shared MAX set", Card: contracts.NewOptCreateCardRequest(contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Question", Answer: contracts.NewStringCardAnswer("Answer")})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sets.JoinSetByShareCode(ctx, memberID, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	if _, err = sets.GetCardsBySetID(ctx, memberID, set.ID); !errors.Is(err, usecase.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for shared member: %v", err)
	}
	list, err := sets.GetCardsBySetID(ctx, maxAccount.User.ID, set.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("author cards: %v %v", list, err)
	}
	if _, err = cards.AnswerQuestion(ctx, memberID, list[0].ID, "true", uuid.New()); err != nil {
		t.Fatal(err)
	}
	facts, err := q.GetFactsBySetID(ctx, pgtype.UUID{Bytes: set.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_fact_progress WHERE user_id=$1 AND fact_id=$2", memberID, facts[0].ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("member progress: %d %v", count, err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_fact_progress WHERE user_id=$1 AND fact_id=$2", maxAccount.User.ID, facts[0].ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("progress leaked to MAX: %d %v", count, err)
	}
	board, err := sets.GetSetLeaderboard(ctx, maxAccount.User.ID, set.ID)
	if err != nil || len(board.Items) != 2 {
		t.Fatalf("shared leaderboard: %+v %v", board, err)
	}
}

func TestDueFactsOnlyForMAXUsers(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	reader := uuid.MustParse("00000000-0000-0000-0000-000000000d01")
	unreachable := uuid.MustParse("00000000-0000-0000-0000-000000000d02")
	set := uuid.MustParse("00000000-0000-0000-0000-000000000d03")
	fact := uuid.MustParse("00000000-0000-0000-0000-000000000d04")
	for _, sql := range []string{
		"INSERT INTO users(id,first_name,max_id) VALUES('" + reader.String() + "','Reader',4242),('" + unreachable.String() + "','Unreachable',NULL)",
		"INSERT INTO sets(id,title,author_id,share_code) VALUES('" + set.String() + "','Due','" + reader.String() + "','424242')",
		"INSERT INTO user_sets(user_id,set_id) VALUES('" + reader.String() + "','" + set.String() + "'),('" + unreachable.String() + "','" + set.String() + "')",
		"INSERT INTO facts(id,set_id,name) VALUES('" + fact.String() + "','" + set.String() + "','Due fact')",
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	// Nobody can be reminded without a MAX ID, so the run does not load them.
	due, err := usecase.NewUserService(db.New(pool)).GetUsersWithDueFacts(ctx, time.Now(), "Europe/Moscow")
	if err != nil || len(due) != 1 || due[0] != reader {
		t.Fatalf("due users: %v %v", due, err)
	}
}
