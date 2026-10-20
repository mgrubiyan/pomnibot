package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestIdentityMigrationAndSharedLearning(t *testing.T) {
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
	maxAccount, err := users.ResolveIdentity(ctx, usecase.Identity{Platform: "max", ExternalID: 9999999, FirstName: "Updated MAX"})
	if err != nil {
		t.Fatal(err)
	}
	if maxAccount.User.ID != 9999999 {
		t.Fatalf("legacy ID changed: %d", maxAccount.User.ID)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_identities WHERE user_id IN (0,1,100001)").Scan(&count); err != nil || count != 0 {
		t.Fatalf("demo identities: %d %v", count, err)
	}
	var elo int
	if err = pool.QueryRow(ctx, "SELECT elo FROM user_fact_progress WHERE user_id=9999999 AND fact_id='legacy-fact'").Scan(&elo); err != nil || elo != 900 {
		t.Fatalf("progress: %d %v", elo, err)
	}
	var owner int64
	if err = pool.QueryRow(ctx, "SELECT author_id FROM sets WHERE id=$1", oldSet).Scan(&owner); err != nil || owner != maxAccount.User.ID {
		t.Fatalf("owner: %d %v", owner, err)
	}
	var wg sync.WaitGroup
	ids := make(chan int64, 32)
	failures := make(chan error, 32)
	for i := range 32 {
		wg.Go(func() {
			account, err := users.ResolveIdentity(ctx, usecase.Identity{Platform: "telegram", ExternalID: 9999999, FirstName: "Remote name", BotStarted: i%2 == 0})
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
	var telegramID int64
	for id := range ids {
		if telegramID == 0 {
			telegramID = id
		}
		if id != telegramID {
			t.Fatal("duplicate Telegram accounts")
		}
	}
	if telegramID <= maxAccount.User.ID {
		t.Fatalf("sequence did not advance: %d", telegramID)
	}
	telegram, err := users.GetAccount(ctx, telegramID)
	if err != nil {
		t.Fatal(err)
	}
	if telegram.ProfileComplete || telegram.User.FirstName != "" {
		t.Fatalf("remote profile was trusted: %+v", telegram)
	}
	if _, err = users.ProfileInput(ctx, telegramID, "Фамилия", false); err != nil {
		t.Fatal(err)
	}
	users = usecase.NewUserService(db.New(pool))
	telegram, err = users.GetAccount(ctx, telegramID)
	if err != nil || telegram.ProfileStep != "first_name" {
		t.Fatalf("lost dialogue: %+v %v", telegram, err)
	}
	telegram, err = users.ProfileInput(ctx, telegramID, "Имя", false)
	if err != nil || !telegram.ProfileComplete || telegram.User.FirstName != "Имя" || telegram.User.LastName.Value != "Фамилия" {
		t.Fatalf("profile: %+v %v", telegram, err)
	}
	if _, err = users.ProfileInput(ctx, telegramID, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err = users.ProfileInput(ctx, telegramID, "Новая фамилия", false); err != nil {
		t.Fatal(err)
	}
	telegram, err = users.GetAccount(ctx, telegramID)
	if err != nil || telegram.User.LastName.Value != "Фамилия" {
		t.Fatalf("partial edit overwrote profile: %+v %v", telegram, err)
	}
	telegram, err = users.SaveProfile(ctx, telegramID, " Форма ", " Новая ")
	if err != nil || telegram.ProfileStep != "" {
		t.Fatalf("form did not clear dialogue: %+v %v", telegram, err)
	}
	if _, err = users.ProfileInput(ctx, telegramID, "Stale answer", false); err != nil {
		t.Fatal(err)
	}
	telegram, err = users.ResolveIdentity(ctx, usecase.Identity{Platform: "telegram", ExternalID: 9999999, FirstName: "Remote", LastName: "Remote"})
	if err != nil || telegram.User.FirstName != "Форма" || telegram.User.LastName.Value != "Новая" {
		t.Fatalf("profile overwritten: %+v %v", telegram, err)
	}
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	cards := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	set, err := sets.CreateSet(ctx, maxAccount.User.ID, &contracts.CreateSetRequest{Title: "Shared MAX set", Card: contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Question", Answer: contracts.NewStringCardAnswer("Answer")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sets.JoinSetByShareCode(ctx, telegramID, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	list, err := sets.GetCardsBySetID(ctx, telegramID, set.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("shared cards: %v %v", list, err)
	}
	if _, err = cards.AnswerQuestion(ctx, telegramID, list[0].ID, "Answer"); err != nil {
		t.Fatal(err)
	}
	facts, err := q.GetFactsBySetID(ctx, pgtype.UUID{Bytes: set.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_fact_progress WHERE user_id=$1 AND fact_id=$2", telegramID, facts[0].ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Telegram progress: %d %v", count, err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_fact_progress WHERE user_id=$1 AND fact_id=$2", maxAccount.User.ID, facts[0].ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("progress leaked to MAX: %d %v", count, err)
	}
	board, err := sets.GetSetLeaderboard(ctx, maxAccount.User.ID, set.ID)
	if err != nil || len(board.Items) != 2 {
		t.Fatalf("shared leaderboard: %+v %v", board, err)
	}
	identities, err := users.GetDeliveryIdentities(ctx, telegramID)
	if err != nil || len(identities) != 1 || identities[0].ExternalID != 9999999 {
		t.Fatalf("delivery: %+v %v", identities, err)
	}
}

func TestTelegramReminderEligibility(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	users := usecase.NewUserService(db.New(pool))
	account, err := users.ResolveIdentity(ctx, usecase.Identity{Platform: "telegram", ExternalID: 42})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		identities, err := users.GetDeliveryIdentities(ctx, account.User.ID)
		if err != nil || len(identities) != want {
			t.Fatalf("delivery=%+v %v, want %d", identities, err, want)
		}
	}
	check(0)
	if _, err = users.SaveProfile(ctx, account.User.ID, "First", "Last"); err != nil {
		t.Fatal(err)
	}
	check(0)
	if _, err = users.ResolveIdentity(ctx, usecase.Identity{Platform: "telegram", ExternalID: 42, BotStarted: true}); err != nil {
		t.Fatal(err)
	}
	check(1)
}
