package repository_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func postgresTestDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("pomnibot_test"),
		postgres.WithUsername("pomnibot"),
		postgres.WithPassword("pomnibot"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start test PostgreSQL (Docker must be running): %v", err)
	}
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, url
}

func manualTestPool(t *testing.T, beforeMigration ...func(*pgxpool.Pool)) *pgxpool.Pool {
	t.Helper()
	pool, _ := postgresTestDatabase(t)
	ctx := t.Context()
	entries, err := repository.MigrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		// Schema only: the demo sets and their fixes would fill the tables.
		if strings.Contains(entry.Name(), "seed") || strings.Contains(entry.Name(), "demo") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "00008") {
			for _, before := range beforeMigration {
				before(pool)
			}
		}
		data, err := repository.MigrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, strings.Split(string(data), "-- +goose Down")[0]); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestManualCardsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	svc := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	user11 := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	user22 := uuid.MustParse("00000000-0000-0000-0000-000000000022")
	user33 := uuid.MustParse("00000000-0000-0000-0000-000000000033")
	for _, id := range []uuid.UUID{user11, user22, user33} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	table := contracts.TableLayout{Columns: []string{"Second", "First"}, Items: []contracts.TableItem{{Text: "b", Column: "Second"}, {Text: "a", Column: "First"}}}
	requests := []contracts.CreateCardRequest{
		{Kind: contracts.CardKindFlip, Question: "Flip", Answer: contracts.NewStringCardAnswer("Answer")},
		{Kind: contracts.CardKindInput, Question: "Input", Answer: contracts.NewStringCardAnswer("Answer")},
		{Kind: contracts.CardKindBoolean, Question: "Boolean", Answer: contracts.NewBoolCardAnswer(false)},
		{Kind: contracts.CardKindChoice, Question: "Choice", Options: []string{"B", "A", "C"}, Answer: contracts.NewStringArrayCardAnswer([]string{"B", "A"})},
		{Kind: contracts.CardKindTable, Question: "Table", Table: contracts.NewOptTableLayout(table), Answer: contracts.NewTableLayoutCardAnswer(table)},
	}
	if _, err := sets.CreateSet(ctx, user11, &contracts.CreateSetRequest{Title: " ", Card: contracts.NewOptCreateCardRequest(requests[0])}); !errors.Is(err, usecase.ErrValidation) {
		t.Fatalf("blank title: %v", err)
	}
	set, err := sets.CreateSet(ctx, user11, &contracts.CreateSetRequest{Title: " Manual ", Card: contracts.NewOptCreateCardRequest(requests[0])})
	if err != nil {
		t.Fatal(err)
	}
	if set.Title != "Manual" || set.CardsTotal != 1 || set.CardsDue != 1 || set.Author.ID != user11 || len(set.ShareCode.Value) != 6 {
		t.Fatalf("unexpected set: %+v", set)
	}
	pgSet := pgtype.UUID{Bytes: set.ID, Valid: true}
	if _, err = sets.JoinSetByShareCode(ctx, user22, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	existing, err := q.GetFactsBySetID(ctx, pgSet)
	if err != nil {
		t.Fatal(err)
	}
	oldProgress, err := q.UpsertFactProgress(ctx, db.UpsertFactProgressParams{
		UserID:         pgtype.UUID{Bytes: user22, Valid: true},
		FactID:         existing[0].ID,
		Step:           1,
		Stability:      2.0,
		Difficulty:     3.0,
		Reps:           1,
		Lapses:         0,
		State:          1,
		LastReviewedAt: pgtype.Timestamptz{Time: time.Now().Truncate(time.Microsecond), Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour).Truncate(time.Microsecond), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(requests); i++ {
		if _, err = svc.CreateCard(ctx, user11, set.ID, &requests[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sets.GetCardsBySetID(ctx, user11, set.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("cards: %d", len(got))
	}
	for i, card := range got {
		if card.Kind != requests[i].Kind || card.Question != requests[i].Question || !reflect.DeepEqual(card.Answer.Value, requests[i].Answer) || card.FactId == uuid.Nil || card.FactName == "" || card.SourceQuote.Value != "" || card.Explanation.Value != "" {
			t.Fatalf("roundtrip %d: %+v", i, card)
		}
		if i == 3 && !reflect.DeepEqual(card.Options, requests[i].Options) {
			t.Fatal("option order changed")
		}
		if i == 4 && !reflect.DeepEqual(card.Table.Value.Columns, table.Columns) {
			t.Fatal("column order changed")
		}
	}
	facts, err := q.GetFactsBySetID(ctx, pgSet)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 5 {
		t.Fatalf("facts: %d", len(facts))
	}
	for _, fact := range facts {
		if !fact.ID.Valid {
			t.Fatal("fact ID invalid")
		}
	}
	progress, err := q.GetUserFactProgress(ctx, db.GetUserFactProgressParams{UserID: pgtype.UUID{Bytes: user22, Valid: true}, FactID: existing[0].ID})
	if err != nil || !reflect.DeepEqual(progress, oldProgress) {
		t.Fatalf("old progress changed: %v", err)
	}
	feed, err := q.GetFeedCardsForUser(ctx, pgtype.UUID{Bytes: user22, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 4 {
		t.Fatalf("new cards missing from member feed: %d", len(feed))
	}
	if _, err = svc.AnswerQuestion(ctx, user22, got[1].ID, "Answer", uuid.New()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{user22, user33} {
		if _, err = svc.CreateCard(ctx, id, set.ID, &requests[0]); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("write by %v: %v", id, err)
		}
	}
	if _, err = svc.CreateCard(ctx, user11, uuid.New(), &requests[0]); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatal(err)
	}
	if err = svc.DeleteCard(ctx, user22, got[0].ID); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatal(err)
	}
	if err = svc.DeleteCard(ctx, user11, got[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.GetUserFactProgress(ctx, db.GetUserFactProgressParams{UserID: pgtype.UUID{Bytes: user22, Valid: true}, FactID: existing[0].ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("orphan progress: %v", err)
	}
	plan, err := sets.GetSetPlan(ctx, user22, set.ID)
	if err != nil || len(plan) != 4 {
		t.Fatalf("orphan fact in plan: %d %v", len(plan), err)
	}
	// Generated facts can contain several cards; deleting one must preserve the others.
	factID := uuid.Must(uuid.NewV7()).String()
	generated, _, err := repository.SaveGeneratedSet(ctx, q, user11, "Generated", repository.GeneratedSet{Facts: []cards.Fact{{ID: factID, Name: "Fact"}}, Cards: []cards.Card{{FactID: factID, Kind: cards.KindFlip, Question: "Q1", Answer: "A"}, {FactID: factID, Kind: cards.KindInput, Question: "Q2", Answer: "A"}}})
	if err != nil {
		t.Fatal(err)
	}
	generatedID := uuid.UUID(generated.ID.Bytes)
	if _, err = svc.CreateCard(ctx, user11, generatedID, &requests[0]); err != nil {
		t.Fatal(err)
	}
	generatedCards, err := sets.GetCardsBySetID(ctx, user11, generatedID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteCard(ctx, user11, generatedCards[0].ID); err != nil {
		t.Fatal(err)
	}
	generatedFacts, err := q.GetFactsBySetID(ctx, generated.ID)
	if err != nil || len(generatedFacts) != 2 {
		t.Fatalf("shared fact removed: %v", err)
	}
	if err = svc.DeleteCard(ctx, user11, generatedCards[1].ID); err != nil {
		t.Fatal(err)
	}
	generatedFacts, err = q.GetFactsBySetID(ctx, generated.ID)
	if err != nil || len(generatedFacts) != 1 {
		t.Fatalf("empty generated fact kept: %v", err)
	}
}

func TestManualRollbackPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	writer := repository.NewManualStore(pool)
	user11 := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	if _, err := q.EnsureUser(ctx, pgtype.UUID{Bytes: user11, Valid: true}); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	count := func() []int {
		t.Helper()
		out := []int{}
		for _, table := range []string{"sets", "user_sets", "facts", "cards", "card_options", "card_table_columns", "card_table_items"} {
			var n int
			if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		return out
	}
	flip := contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Q", Answer: contracts.NewStringCardAnswer("A")}
	id, err := writer.CreateSet(ctx, user11, "Valid", &flip)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		table, field string
		req          contracts.CreateCardRequest
	}{
		{"cards", "question", contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "FAIL", Answer: contracts.NewStringCardAnswer("A")}},
		{"card_options", "text", contracts.CreateCardRequest{Kind: contracts.CardKindChoice, Question: "Q", Options: []string{"A", "FAIL"}, Answer: contracts.NewStringCardAnswer("A")}},
		{"card_table_columns", "name", contracts.CreateCardRequest{Kind: contracts.CardKindTable, Question: "Q", Table: contracts.NewOptTableLayout(contracts.TableLayout{Columns: []string{"A", "FAIL"}})}},
		{"card_table_items", "item_text", contracts.CreateCardRequest{Kind: contracts.CardKindTable, Question: "Q", Table: contracts.NewOptTableLayout(contracts.TableLayout{Columns: []string{"A", "B"}, Items: []contracts.TableItem{{Text: "a", Column: "A"}, {Text: "FAIL", Column: "B"}}})}},
	}
	for _, tt := range cases {
		t.Run(tt.table, func(t *testing.T) {
			exec("ALTER TABLE " + tt.table + " ADD CONSTRAINT reject_failure CHECK (" + tt.field + " <> 'FAIL')")
			before := count()
			if _, err := writer.CreateSet(ctx, user11, "Failure", &tt.req); err == nil {
				t.Fatal("expected create-set failure")
			}
			if !reflect.DeepEqual(before, count()) {
				t.Fatal("partial set committed")
			}
			if _, err := writer.CreateCard(ctx, user11, id, &tt.req); err == nil {
				t.Fatal("expected create-card failure")
			}
			if !reflect.DeepEqual(before, count()) {
				t.Fatal("partial card committed")
			}
			exec("ALTER TABLE " + tt.table + " DROP CONSTRAINT reject_failure")
		})
	}
	// A sequence survives rollback, so this trigger simulates two code collisions.
	exec(`CREATE SEQUENCE collisions; CREATE FUNCTION collide() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF nextval('collisions') <= 2 THEN RAISE EXCEPTION 'collision' USING ERRCODE='23505', CONSTRAINT='sets_share_code_key'; END IF; RETURN NEW; END $$; CREATE TRIGGER collide BEFORE INSERT ON sets FOR EACH ROW EXECUTE FUNCTION collide();`)
	if _, err = writer.CreateSet(ctx, user11, "Retry", &flip); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE OR REPLACE FUNCTION collide() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'collision' USING ERRCODE='23505', CONSTRAINT='sets_share_code_key'; END $$;`)
	before := count()
	if _, err = writer.CreateSet(ctx, user11, "Exhausted", &flip); err == nil {
		t.Fatal("expected retry limit")
	}
	if !reflect.DeepEqual(before, count()) {
		t.Fatal("collision left partial data")
	}
}
