//go:build integration

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
	for _, table := range []string{"sets", "user_sets", "user_fact_progress", "answers"} {
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

	// Remove the photos' foreign key before 00009 rebuilds user IDs.
	m20Data, err := repository.MigrationsFS.ReadFile("migrations/00020_avatars.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, strings.Split(string(m20Data), "-- +goose Down")[1]); err != nil {
		t.Fatalf("failed to run 00020 Down: %v", err)
	}

	// Remove the report's foreign key before older migrations rebuild set IDs.
	m14Data, err := repository.MigrationsFS.ReadFile("migrations/00014_processing_reports.sql")
	if err != nil {
		t.Fatal(err)
	}
	m14Parts := strings.Split(string(m14Data), "-- +goose Down")
	if len(m14Parts) != 2 {
		t.Fatalf("expected 2 parts for 00014, got %d", len(m14Parts))
	}
	if _, err := pool.Exec(ctx, m14Parts[1]); err != nil {
		t.Fatalf("failed to run 00014 Down: %v", err)
	}

	// 0a. Roll back 00013 so card_issues foreign keys to users and cards are dropped
	m13Data, err := repository.MigrationsFS.ReadFile("migrations/00013_card_issues.sql")
	if err != nil {
		t.Fatal(err)
	}
	m13Parts := strings.Split(string(m13Data), "-- +goose Down")
	if len(m13Parts) != 2 {
		t.Fatalf("expected 2 parts for 00013, got %d", len(m13Parts))
	}
	m13Up := m13Parts[0]
	m13Down := m13Parts[1]

	if _, err := pool.Exec(ctx, m13Down); err != nil {
		t.Fatalf("failed to run 00013 Down: %v", err)
	}

	// 0b. Roll back 00012 so card_kind enum and FSRS schema are reverted
	m12Data, err := repository.MigrationsFS.ReadFile("migrations/00012_fsrs6_and_card_kind_enum.sql")
	if err != nil {
		t.Fatal(err)
	}
	m12Parts := strings.Split(string(m12Data), "-- +goose Down")
	if len(m12Parts) != 2 {
		t.Fatalf("expected 2 parts for 00012, got %d", len(m12Parts))
	}
	m12Up := m12Parts[0]
	m12Down := m12Parts[1]

	if _, err := pool.Exec(ctx, m12Down); err != nil {
		t.Fatalf("failed to run 00012 Down: %v", err)
	}

	// 0c. Roll back 00011 first so topics exists for 00009 Down
	m11Data, err := repository.MigrationsFS.ReadFile("migrations/00011_drop_topics.sql")
	if err != nil {
		t.Fatal(err)
	}
	m11Parts := strings.Split(string(m11Data), "-- +goose Down")
	if len(m11Parts) != 2 {
		t.Fatalf("expected 2 parts for 00011, got %d", len(m11Parts))
	}
	m11Up := m11Parts[0]
	m11Down := m11Parts[1]

	if _, err := pool.Exec(ctx, m11Down); err != nil {
		t.Fatalf("failed to run 00011 Down: %v", err)
	}

	// 0d. Roll back 00010 so 00009 operates on answer_results
	m10Data, err := repository.MigrationsFS.ReadFile("migrations/00010_rename_answer_results_to_answers.sql")
	if err != nil {
		t.Fatal(err)
	}
	m10Parts := strings.Split(string(m10Data), "-- +goose Down")
	if len(m10Parts) != 2 {
		t.Fatalf("expected 2 parts for 00010, got %d", len(m10Parts))
	}
	m10Up := m10Parts[0]
	m10Down := m10Parts[1]

	if _, err := pool.Exec(ctx, m10Down); err != nil {
		t.Fatalf("failed to run 00010 Down: %v", err)
	}

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

	// 4. Re-run 00010 Up and verify answers table exists and answered_at is dropped
	if _, err := pool.Exec(ctx, m10Up); err != nil {
		t.Fatalf("failed to run 00010 Up after 00009 Up: %v", err)
	}
	var hasAnsweredAt bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='answers' AND column_name='answered_at')").Scan(&hasAnsweredAt); err != nil || hasAnsweredAt {
		t.Fatalf("expected answers.answered_at to NOT exist after 00010 Up, err: %v", err)
	}

	// 5. Re-run 00011 Up and verify topics is dropped
	if _, err := pool.Exec(ctx, m11Up); err != nil {
		t.Fatalf("failed to re-run 00011 Up: %v", err)
	}
	var hasTopics bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='topics')").Scan(&hasTopics); err != nil || hasTopics {
		t.Fatalf("expected topics table to NOT exist after 00011 Up, err: %v", err)
	}

	// 6. Re-run 00012 Up and verify card_kind enum exists
	if _, err := pool.Exec(ctx, m12Up); err != nil {
		t.Fatalf("failed to re-run 00012 Up: %v", err)
	}

	// 7. Re-run 00013 Up and verify card_issues table exists
	if _, err := pool.Exec(ctx, m13Up); err != nil {
		t.Fatalf("failed to re-run 00013 Up: %v", err)
	}
	if _, err := pool.Exec(ctx, m14Parts[0]); err != nil {
		t.Fatalf("failed to re-run 00014 Up: %v", err)
	}
	var hasCardIssues bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='card_issues')").Scan(&hasCardIssues); err != nil || !hasCardIssues {
		t.Fatalf("expected card_issues table to exist after 00013 Up, err: %v", err)
	}
}
