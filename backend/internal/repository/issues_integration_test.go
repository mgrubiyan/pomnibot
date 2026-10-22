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

// The author sees open complaints: an edit answers the ones made before it,
// keeping the card as it is closes them, and nobody else sees them.
func TestOpenCardIssuesPostgres(t *testing.T) {
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
	generated, _, err := repository.SaveGeneratedSet(ctx, q, 11, "Set", generator.Result{
		Facts: []cards.Fact{{ID: "f1", Name: "F1"}, {ID: "f2", Name: "F2"}},
		Cards: []cards.Card{
			{FactID: "f1", Kind: cards.KindFlip, Question: "Q1", Answer: "A1"},
			{FactID: "f2", Kind: cards.KindFlip, Question: "Q2", Answer: "A2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	setID := uuid.UUID(generated.ID.Bytes)
	for _, id := range []int64{22, 33} {
		if _, err = sets.JoinSetByShareCode(ctx, id, generated.ShareCode); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sets.GetCardsBySetID(ctx, 11, setID)
	if err != nil || len(got) != 2 {
		t.Fatalf("cards: %d %v", len(got), err)
	}
	first, second := got[0].ID, got[1].ID
	report := func(user int64, card uuid.UUID, reason contracts.CardIssueReason) {
		t.Helper()
		if err := svc.ReportCardIssue(ctx, user, card, reason); err != nil {
			t.Fatal(err)
		}
	}
	type open struct {
		card   uuid.UUID
		reason contracts.CardIssueReason
		count  int
	}
	openIssues := func() []open {
		t.Helper()
		issues, err := svc.GetSetIssues(ctx, 11, setID)
		if err != nil {
			t.Fatal(err)
		}
		res := []open{}
		for _, i := range issues {
			res = append(res, open{i.CardId, i.Reason, i.Count})
		}
		return res
	}
	sameIssues := func(step string, want ...open) {
		t.Helper()
		got := openIssues()
		if len(got) != len(want) {
			t.Fatalf("%s: open = %+v, want %+v", step, got, want)
		}
		for _, w := range want {
			found := false
			for _, g := range got {
				found = found || g == w
			}
			if !found {
				t.Fatalf("%s: open = %+v, want %+v", step, got, want)
			}
		}
	}

	report(22, first, contracts.CardIssueReasonAnswer)
	report(33, first, contracts.CardIssueReasonAnswer)
	report(22, second, contracts.CardIssueReasonWording)
	sameIssues("reported", open{first, contracts.CardIssueReasonAnswer, 2}, open{second, contracts.CardIssueReasonWording, 1})

	for _, id := range []int64{22, 33} {
		if _, err = svc.GetSetIssues(ctx, id, setID); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("member %d sees complaints: %v", id, err)
		}
		if err = svc.ResolveCardIssues(ctx, id, first); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("member %d closes complaints: %v", id, err)
		}
	}

	if _, err = svc.UpdateCard(ctx, 11, first, &contracts.UpdateCardRequest{Question: contracts.NewOptString("Q1 fixed")}); err != nil {
		t.Fatal(err)
	}
	sameIssues("edited", open{second, contracts.CardIssueReasonWording, 1})
	report(33, first, contracts.CardIssueReasonOther)
	sameIssues("reported after the edit", open{first, contracts.CardIssueReasonOther, 1}, open{second, contracts.CardIssueReasonWording, 1})

	if err = svc.ResolveCardIssues(ctx, 11, second); err != nil {
		t.Fatal(err)
	}
	sameIssues("kept", open{first, contracts.CardIssueReasonOther, 1})
	if err = svc.DeleteCard(ctx, 11, first); err != nil {
		t.Fatal(err)
	}
	sameIssues("deleted")
	if err = svc.ResolveCardIssues(ctx, 11, first); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("deleted card: %v", err)
	}

	var stored int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM card_issues").Scan(&stored); err != nil || stored != 4 {
		t.Fatalf("stored complaints = %d, %v; closing must not delete them", stored, err)
	}
}
