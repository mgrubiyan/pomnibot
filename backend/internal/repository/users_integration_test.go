package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Start with the schema on main, where users.id is the MAX ID.
func TestUsersMigrationPreservesLegacyMAXData(t *testing.T) {
	ctx := context.Background()
	snapshots := make(map[string]string)
	pool := manualTestPool(t, func(pool *pgxpool.Pool) {
		for _, sql := range []string{
			"INSERT INTO users(id,first_name,last_name,username) VALUES(42,'Same','Name','same')",
			"INSERT INTO sets(id,title,author_id,share_code) VALUES('00000000-0000-0000-0000-000000000042','Existing',42,'424242')",
			"INSERT INTO user_sets(user_id,set_id) VALUES(42,'00000000-0000-0000-0000-000000000042')",
			"INSERT INTO facts(id,set_id,name) VALUES('saved-fact','00000000-0000-0000-0000-000000000042','Saved')",
			"INSERT INTO cards(id,fact_id,kind,question,answer_text,explanation,source_quote) VALUES('00000000-0000-0000-0000-000000000043','saved-fact','flip','Q','A','','')",
			"INSERT INTO user_fact_progress(user_id,fact_id,elo) VALUES(42,'saved-fact',900)",
			"INSERT INTO answer_results(user_id,card_id,correct,answered_at) VALUES(42,'00000000-0000-0000-0000-000000000043',true,CURRENT_TIMESTAMP)",
		} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
	})
	for _, table := range []string{"sets", "user_sets", "user_fact_progress", "answer_results"} {
		var snapshot string
		if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text FROM "+table+" t").Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		snapshots[table] = snapshot
	}
	users := usecase.NewUserService(db.New(pool))
	username := "same"
	maxUser, err := users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 42, FirstName: "Same", Username: &username})
	if err != nil || maxUser.MaxID != 42 || maxUser.TgID != 0 {
		t.Fatalf("legacy MAX account changed: %+v %v", maxUser, err)
	}
	var authorID [16]byte
	if err := pool.QueryRow(ctx, "SELECT author_id FROM sets WHERE share_code='424242'").Scan(&authorID); err != nil || authorID != maxUser.User.ID {
		t.Fatalf("set author_id not updated to user UUID: %v vs %v, %v", authorID, maxUser.User.ID, err)
	}
	tgUser, err := users.UpsertTelegramUser(ctx, usecase.TelegramUserParams{TgID: 42, Username: username, BotStarted: true})
	if err != nil || tgUser.User.ID == maxUser.User.ID || tgUser.TgID != 42 || tgUser.MaxID != 0 {
		t.Fatalf("platform IDs mixed: MAX=%+v TG=%+v err=%v", maxUser, tgUser, err)
	}
	tgUser, err = users.SaveProfile(ctx, tgUser.User.ID, "Same", "Name")
	if err != nil {
		t.Fatal(err)
	}
	// Identical names and usernames must not merge the accounts on re-entry.
	again, err := users.UpsertTelegramUser(ctx, usecase.TelegramUserParams{TgID: 42, Username: username})
	if err != nil || again.User.ID != tgUser.User.ID || !again.ProfileComplete {
		t.Fatalf("Telegram re-entry: %+v %v", again, err)
	}
	again, err = users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 42})
	if err != nil || again.User.ID != maxUser.User.ID {
		t.Fatalf("MAX re-entry: %+v %v", again, err)
	}
	// A messenger ID matching another account's internal ID is still a new user.
	collision, err := users.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: 99999, FirstName: "Same"})
	if err != nil || collision.User.ID == tgUser.User.ID || collision.User.ID == maxUser.User.ID {
		t.Fatalf("internal ID used as external lookup: %+v %v", collision, err)
	}
	for table, before := range snapshots {
		var after string
		if err := pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text FROM "+table+" t").Scan(&after); err != nil || after != before {
			t.Fatalf("%s changed: before=%s after=%s err=%v", table, before, after, err)
		}
	}
}

func TestMigration00009UpDown(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()

	data, err := repository.MigrationsFS.ReadFile("migrations/00009_unify_uuidv7_ids.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	upSQL := parts[0]
	downSQL := parts[1]

	// 1. Run Down migration
	if _, err := pool.Exec(ctx, downSQL); err != nil {
		t.Fatalf("failed to run 00009 Down: %v", err)
	}

	// 2. Verify reverted column types
	var usersIDType string
	if err := pool.QueryRow(ctx, "SELECT data_type FROM information_schema.columns WHERE table_name='users' AND column_name='id'").Scan(&usersIDType); err != nil || usersIDType != "bigint" {
		t.Fatalf("expected users.id to be bigint, got %q, err: %v", usersIDType, err)
	}

	var factsIDType string
	if err := pool.QueryRow(ctx, "SELECT data_type FROM information_schema.columns WHERE table_name='facts' AND column_name='id'").Scan(&factsIDType); err != nil || factsIDType != "text" {
		t.Fatalf("expected facts.id to be text, got %q, err: %v", factsIDType, err)
	}

	for _, tbl := range []string{"users", "sets", "topics", "facts", "cards", "answer_results"} {
		var hasCreatedAt bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name='created_at')", tbl).Scan(&hasCreatedAt); err != nil || !hasCreatedAt {
			t.Fatalf("expected %s.created_at to exist after Down, err: %v", tbl, err)
		}
	}

	// 3. Re-run Up migration to verify roundtrip idempotency
	if _, err := pool.Exec(ctx, upSQL); err != nil {
		t.Fatalf("failed to re-run 00009 Up after Down: %v", err)
	}

	// Verify users.id is UUID again
	if err := pool.QueryRow(ctx, "SELECT data_type FROM information_schema.columns WHERE table_name='users' AND column_name='id'").Scan(&usersIDType); err != nil || usersIDType != "uuid" {
		t.Fatalf("expected users.id to be uuid after re-running Up, got %q, err: %v", usersIDType, err)
	}
}
