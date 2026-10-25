package repository_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestSetRetentionPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	cardService := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	owner, member, stranger := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, stranger} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	generated, _, err := repository.SaveGeneratedSet(ctx, q, owner, "Retention", repository.GeneratedSet{
		Facts: []cards.Fact{{ID: "01900000-0000-7000-8000-000000000005", Name: "Several cards"}, {ID: "01900000-0000-7000-8000-000000000006", Name: "One card"}},
		Cards: []cards.Card{
			{FactID: "01900000-0000-7000-8000-000000000005", Kind: cards.KindFlip, Question: "Easy", Answer: "A"},
			{FactID: "01900000-0000-7000-8000-000000000005", Kind: cards.KindInput, Question: "Hard", Answer: "B"},
			{FactID: "01900000-0000-7000-8000-000000000006", Kind: cards.KindFlip, Question: "Only", Answer: "C"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	setID := uuid.UUID(generated.ID.Bytes)
	if _, err = sets.JoinSetByShareCode(ctx, member, generated.ShareCode); err != nil {
		t.Fatal(err)
	}
	if _, err = q.CreateFact(ctx, db.CreateFactParams{ID: pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}, SetID: generated.ID, Name: "No cards"}); err != nil {
		t.Fatal(err)
	}
	all, err := sets.GetCardsBySetID(ctx, owner, setID)
	if err != nil {
		t.Fatal(err)
	}
	byQuestion := make(map[string]uuid.UUID)
	for _, c := range all {
		byQuestion[c.Question] = c.ID
	}
	// Reporting a harder card leaves an easier card available. Reporting the
	// only card blocks that fact; the author's complaints never block theirs.
	for _, tc := range []struct {
		user     uuid.UUID
		question string
	}{{member, "Hard"}, {member, "Only"}, {owner, "Easy"}} {
		if err = cardService.ReportCardIssue(ctx, tc.user, byQuestion[tc.question], contracts.CardIssueReasonAnswer, ""); err != nil {
			t.Fatal(err)
		}
	}
	query := db.GetSetRetentionFactsParams{SetID: generated.ID, UserID: pgtype.UUID{Bytes: member, Valid: true}}
	rows, err := q.GetSetRetentionFacts(ctx, query)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if !rows[0].Available || rows[1].Available || rows[2].Available {
		t.Fatalf("availability: %+v", rows)
	}
	for _, user := range []uuid.UUID{owner, member} {
		forecast, err := sets.GetSetRetention(ctx, user, setID)
		if err != nil || forecast.TotalFacts != 3 || forecast.Current != 0 || forecast.Groups.Below70 != 3 {
			t.Fatalf("forecast=%+v err=%v", forecast, err)
		}
	}
	if _, err = sets.GetSetRetention(ctx, stranger, setID); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	if _, err = sets.GetSetRetention(ctx, owner, uuid.New()); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("absent: %v", err)
	}
	// Preserve a real learned state, and verify both it and answer history
	// remain byte-for-byte equivalent after multiple forecast calculations.
	_, err = q.UpsertFactProgress(ctx, db.UpsertFactProgressParams{
		UserID: query.UserID, FactID: rows[0].ID, Step: 1, Stability: 2, Difficulty: 5, Reps: 2, State: 2,
		LastReviewedAt: pgtype.Timestamptz{Time: time.Now().Add(-48 * time.Hour), Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := q.GetSetRetentionFacts(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	var answersBefore, answersAfter int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM answers").Scan(&answersBefore); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = sets.GetSetRetention(ctx, member, setID); err != nil {
			t.Fatal(err)
		}
	}
	after, err := q.GetSetRetentionFacts(ctx, query)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("state mutated: %v", err)
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM answers").Scan(&answersAfter); err != nil || answersBefore != answersAfter {
		t.Fatalf("answers mutated: %v", err)
	}
	if err = cardService.ResolveCardIssues(ctx, owner, byQuestion["Only"]); err != nil {
		t.Fatal(err)
	}
	after, err = q.GetSetRetentionFacts(ctx, query)
	if err != nil || !after[1].Available {
		t.Fatalf("resolved issue still blocks: %v", err)
	}
}
