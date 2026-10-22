package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// A complaint is kept with a copy of the card as the reader saw it: the
// author fixing or deleting the card must not erase what was wrong.
func TestCardIssuesPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	svc := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	for _, id := range []int64{11, 22, 33} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	// Complaints are mostly about generated cards, which carry a quote.
	generated, _, err := repository.SaveGeneratedSet(ctx, q, 11, "Set", generator.Result{
		Facts: []cards.Fact{{ID: "fact", Name: "Fact"}},
		Cards: []cards.Card{{FactID: "fact", Kind: cards.KindInput, Question: "Q", Answer: "A", Explanation: "E", SourceQuote: "Quote"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	setID := uuid.UUID(generated.ID.Bytes)
	if _, err = sets.JoinSetByShareCode(ctx, 22, generated.ShareCode); err != nil {
		t.Fatal(err)
	}
	got, err := sets.GetCardsBySetID(ctx, 11, setID)
	if err != nil || len(got) != 1 {
		t.Fatalf("cards: %d %v", len(got), err)
	}
	cardID := got[0].ID

	if err = svc.ReportCardIssue(ctx, 33, cardID, contracts.CardIssueReasonAnswer); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("outsider: %v", err)
	}
	if err = svc.ReportCardIssue(ctx, 22, cardID, contracts.CardIssueReasonNotInNotes); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReportCardIssue(ctx, 11, cardID, contracts.CardIssueReasonWording); err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteCard(ctx, 11, cardID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReportCardIssue(ctx, 22, cardID, contracts.CardIssueReasonOther); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("deleted card: %v", err)
	}
	if err = sets.DeleteSet(ctx, 11, setID); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT user_id, reason, kind, question, answer_text, explanation, source_quote, card_id IS NULL, set_id IS NULL
		FROM card_issues ORDER BY created_at, user_id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type issue struct {
		user                           int64
		reason, kind, question, answer string
		explanation, quote             string
		cardGone, setGone              bool
	}
	var issues []issue
	for rows.Next() {
		var i issue
		if err = rows.Scan(&i.user, &i.reason, &i.kind, &i.question, &i.answer, &i.explanation, &i.quote, &i.cardGone, &i.setGone); err != nil {
			t.Fatal(err)
		}
		issues = append(issues, i)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []issue{
		{22, "not-in-notes", "input", "Q", "A", "E", "Quote", true, true},
		{11, "wording", "input", "Q", "A", "E", "Quote", true, true},
	}
	if len(issues) != len(want) {
		t.Fatalf("issues = %+v, want %+v", issues, want)
	}
	for i := range want {
		if issues[i] != want[i] {
			t.Errorf("issue %d = %+v, want %+v", i, issues[i], want[i])
		}
	}
}
