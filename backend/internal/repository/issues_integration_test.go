package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{u1, u2, u3} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	// Complaints are mostly about generated cards, which carry a quote.
	generated, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Set", generator.Result{
		Facts: []cards.Fact{{ID: "fact", Name: "Fact"}},
		Cards: []cards.Card{{FactID: "fact", Kind: cards.KindInput, Question: "Q", Answer: "A", Explanation: "E", SourceQuote: "Quote"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	setID := uuid.UUID(generated.ID.Bytes)
	if _, err = sets.JoinSetByShareCode(ctx, u2, generated.ShareCode); err != nil {
		t.Fatal(err)
	}
	got, err := sets.GetCardsBySetID(ctx, u1, setID)
	if err != nil || len(got) != 1 {
		t.Fatalf("cards: %d %v", len(got), err)
	}
	cardID := got[0].ID

	if err = svc.ReportCardIssue(ctx, u3, cardID, contracts.CardIssueReasonAnswer); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("outsider: %v", err)
	}
	if err = svc.ReportCardIssue(ctx, u2, cardID, contracts.CardIssueReasonNotInNotes); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReportCardIssue(ctx, u1, cardID, contracts.CardIssueReasonWording); err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteCard(ctx, u1, cardID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReportCardIssue(ctx, u2, cardID, contracts.CardIssueReasonOther); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("deleted card: %v", err)
	}
	if err = sets.DeleteSet(ctx, u1, setID); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT user_id, reason, kind, question, answer_text, explanation, source_quote, card_id IS NULL, set_id IS NULL
		FROM card_issues ORDER BY created_at, user_id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type issue struct {
		user                           uuid.UUID
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
		{u2, "not-in-notes", "input", "Q", "A", "E", "Quote", true, true},
		{u1, "wording", "input", "Q", "A", "E", "Quote", true, true},
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
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{u1, u2, u3} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	generated, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Set", generator.Result{
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
	for _, id := range []uuid.UUID{u2, u3} {
		if _, err = sets.JoinSetByShareCode(ctx, id, generated.ShareCode); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sets.GetCardsBySetID(ctx, u1, setID)
	if err != nil || len(got) != 2 {
		t.Fatalf("cards: %d %v", len(got), err)
	}
	first, second := got[0].ID, got[1].ID
	report := func(user uuid.UUID, card uuid.UUID, reason contracts.CardIssueReason) {
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
		issues, err := svc.GetSetIssues(ctx, u1, setID)
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

	report(u2, first, contracts.CardIssueReasonAnswer)
	report(u3, first, contracts.CardIssueReasonAnswer)
	report(u2, second, contracts.CardIssueReasonWording)
	// The card comes back in the next session, and the same reader may
	// complain again: the count is of people.
	report(u2, second, contracts.CardIssueReasonWording)
	sameIssues("reported", open{first, contracts.CardIssueReasonAnswer, 2}, open{second, contracts.CardIssueReasonWording, 1})

	for _, id := range []uuid.UUID{u2, u3} {
		if _, err = svc.GetSetIssues(ctx, id, setID); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("member %s sees complaints: %v", id, err)
		}
		if err = svc.ResolveCardIssues(ctx, id, first); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("member %s closes complaints: %v", id, err)
		}
	}

	if _, err = svc.UpdateCard(ctx, u1, first, &contracts.UpdateCardRequest{Question: contracts.NewOptString("Q1 fixed")}); err != nil {
		t.Fatal(err)
	}
	sameIssues("edited", open{second, contracts.CardIssueReasonWording, 1})
	report(u3, first, contracts.CardIssueReasonOther)
	sameIssues("reported after the edit", open{first, contracts.CardIssueReasonOther, 1}, open{second, contracts.CardIssueReasonWording, 1})

	if err = svc.ResolveCardIssues(ctx, u1, second); err != nil {
		t.Fatal(err)
	}
	sameIssues("kept", open{first, contracts.CardIssueReasonOther, 1})
	if err = svc.DeleteCard(ctx, u1, first); err != nil {
		t.Fatal(err)
	}
	sameIssues("deleted")
	if err = svc.ResolveCardIssues(ctx, u1, first); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("deleted card: %v", err)
	}

	var stored int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM card_issues").Scan(&stored); err != nil || stored != 5 {
		t.Fatalf("stored complaints = %d, %v; closing must not delete them", stored, err)
	}
}

// A reader does not get a card they complained about until the author
// answers the complaint; the fact goes on with its other cards.
func TestFeedSkipsReportedCardPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	svc := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{u1, u2, u3} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	generated, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Set", generator.Result{
		Facts: []cards.Fact{{ID: "f1", Name: "F1"}, {ID: "f2", Name: "F2"}},
		Cards: []cards.Card{
			{FactID: "f1", Kind: cards.KindFlip, Question: "Easy", Answer: "A0"},
			{FactID: "f1", Kind: cards.KindInput, Question: "Input", Answer: "A1"},
			{FactID: "f2", Kind: cards.KindFlip, Question: "Only", Answer: "A2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{u2, u3} {
		if _, err = sets.JoinSetByShareCode(ctx, id, generated.ShareCode); err != nil {
			t.Fatal(err)
		}
	}
	setID := uuid.UUID(generated.ID.Bytes)
	byQuestion := map[string]uuid.UUID{}
	all, err := sets.GetCardsBySetID(ctx, u1, setID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		byQuestion[c.Question] = c.ID
	}
	feed := func(user uuid.UUID) []string {
		t.Helper()
		rows, err := q.GetFeedCardsForUser(ctx, pgtype.UUID{Bytes: user, Valid: true})
		if err != nil {
			t.Fatal(err)
		}
		questions := []string{}
		for _, r := range rows {
			questions = append(questions, r.Question)
		}
		slices.Sort(questions)
		return questions
	}
	sameFeed := func(step string, user uuid.UUID, want ...string) {
		t.Helper()
		if got := feed(user); !slices.Equal(got, want) {
			t.Fatalf("%s: feed of %s = %v, want %v", step, user, got, want)
		}
	}

	// Step 0 for f1 is "Easy", f2 is "Only"
	sameFeed("start", u2, "Easy", "Only")

	// Advance u2 to step 1 on f1 by updating progress
	f1Rows, err := q.GetFactsBySetID(ctx, generated.ID)
	if err != nil || len(f1Rows) < 1 {
		t.Fatal(err)
	}
	var f1ID pgtype.UUID
	for _, f := range f1Rows {
		if f.Name == "F1" {
			f1ID = f.ID
			break
		}
	}
	_, err = pool.Exec(ctx, "INSERT INTO user_fact_progress (user_id, fact_id, step) VALUES ($1, $2, 1) ON CONFLICT (user_id, fact_id) DO UPDATE SET step = 1", u2, f1ID)
	if err != nil {
		t.Fatal(err)
	}
	sameFeed("advanced", u2, "Input", "Only")

	// Report the harder card "Input"
	if err = svc.ReportCardIssue(ctx, u2, byQuestion["Input"], contracts.CardIssueReasonAnswer); err != nil {
		t.Fatal(err)
	}
	// "Input" is hidden, and step rolled back to 0 -> "Easy" and "Only" shown!
	sameFeed("reported", u2, "Easy", "Only")
	sameFeed("someone else", u3, "Easy", "Only")

	// The author's own complaint waits in «Жалобы»; the card stays theirs.
	if err = svc.ReportCardIssue(ctx, u1, byQuestion["Easy"], contracts.CardIssueReasonWording); err != nil {
		t.Fatal(err)
	}
	sameFeed("author", u1, "Easy", "Only")

	// When author edits "Input", it becomes available again
	if _, err = svc.UpdateCard(ctx, u1, byQuestion["Input"], &contracts.UpdateCardRequest{Explanation: contracts.NewOptString("Fixed")}); err != nil {
		t.Fatal(err)
	}
	// And if user step advances back to 1:
	_, err = pool.Exec(ctx, "UPDATE user_fact_progress SET step = 1 WHERE user_id = $1 AND fact_id = $2", u2, f1ID)
	if err != nil {
		t.Fatal(err)
	}
	sameFeed("answered", u2, "Input", "Only")
}

// The author hears about a card once: the first reader's complaint asks to
// tell them, the next ones wait until the author edits the card or keeps it.
// The author's own complaint neither asks nor silences the readers'.
func TestCardIssueNotifiesAuthorOncePostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	svc := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{u1, u2, u3} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	generated, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Биология", generator.Result{
		Facts: []cards.Fact{{ID: "f1", Name: "F1"}},
		Cards: []cards.Card{{FactID: "f1", Kind: cards.KindFlip, Question: "Q1", Answer: "A1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{u2, u3} {
		if _, err = sets.JoinSetByShareCode(ctx, id, generated.ShareCode); err != nil {
			t.Fatal(err)
		}
	}
	all, err := sets.GetCardsBySetID(ctx, u1, uuid.UUID(generated.ID.Bytes))
	if err != nil || len(all) != 1 {
		t.Fatalf("cards: %d %v", len(all), err)
	}
	card := all[0].ID
	report := func(step string, user uuid.UUID, want bool) {
		t.Helper()
		row, err := q.CreateCardIssue(ctx, db.CreateCardIssueParams{
			UserID: pgtype.UUID{Bytes: user, Valid: true},
			Reason: db.CardIssueReasonAnswer,
			CardID: pgtype.UUID{Bytes: card, Valid: true},
		})
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if uuid.UUID(row.AuthorID.Bytes) != u1 || row.SetTitle != "Биология" {
			t.Fatalf("%s: row = %+v", step, row)
		}
		if row.NotifyAuthor != want {
			t.Fatalf("%s: notify = %v, want %v", step, row.NotifyAuthor, want)
		}
	}

	report("author first", u1, false)
	report("first reader", u2, true)
	report("second reader", u3, false)
	report("first reader again", u2, false)
	if _, err = svc.UpdateCard(ctx, u1, card, &contracts.UpdateCardRequest{Explanation: contracts.NewOptString("Fixed")}); err != nil {
		t.Fatal(err)
	}
	report("after the edit", u3, true)
	if err = svc.ResolveCardIssues(ctx, u1, card); err != nil {
		t.Fatal(err)
	}
	report("after «Всё верно»", u2, true)

	if _, err = q.CreateCardIssue(ctx, db.CreateCardIssueParams{UserID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Reason: db.CardIssueReasonAnswer, CardID: pgtype.UUID{Bytes: card, Valid: true}}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("outsider: %v", err)
	}
}

// Admins see every complaint with what became of it.
func TestListCardIssuesPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	writer := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, writer)
	svc := usecase.NewCardService(q, users, usecase.WithManualCards(writer))
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{u1, u2, u3} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET first_name = 'Анна', last_name = 'Иванова' WHERE id = $1", u2); err != nil {
		t.Fatal(err)
	}
	questions := []string{"open", "fixed", "kept", "kept-then-edited", "fixed-then-kept", "fixed-kept-edited", "deleted"}
	result := generator.Result{Facts: []cards.Fact{{ID: "f", Name: "F"}}}
	for _, question := range questions {
		result.Cards = append(result.Cards, cards.Card{FactID: "f", Kind: cards.KindFlip, Question: question, Answer: "A"})
	}
	kept, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Биология", result)
	if err != nil {
		t.Fatal(err)
	}
	gone, _, err := repository.SaveGeneratedSet(ctx, q, u1, "Удалённый", generator.Result{
		Facts: []cards.Fact{{ID: "g", Name: "G"}},
		Cards: []cards.Card{{FactID: "g", Kind: cards.KindInput, Question: "set-deleted", Answer: "A", Explanation: "E", SourceQuote: "Quote"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []*db.Set{kept, gone} {
		for _, id := range []uuid.UUID{u2, u3} {
			if _, err = sets.JoinSetByShareCode(ctx, id, set.ShareCode); err != nil {
				t.Fatal(err)
			}
		}
	}
	card := map[string]uuid.UUID{}
	for _, set := range []*db.Set{kept, gone} {
		got, err := sets.GetCardsBySetID(ctx, u1, uuid.UUID(set.ID.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range got {
			card[c.Question] = c.ID
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	report := func(user uuid.UUID, question string) {
		t.Helper()
		must(svc.ReportCardIssue(ctx, user, card[question], contracts.CardIssueReasonAnswer))
	}
	edit := func(question string) {
		t.Helper()
		_, err := svc.UpdateCard(ctx, u1, card[question], &contracts.UpdateCardRequest{Explanation: contracts.NewOptString("Fixed")})
		must(err)
	}
	for _, question := range append(questions, "set-deleted") {
		report(u2, question)
	}
	edit("fixed")
	must(svc.ResolveCardIssues(ctx, u1, card["kept"]))
	must(svc.ResolveCardIssues(ctx, u1, card["kept-then-edited"]))
	edit("kept-then-edited")
	edit("fixed-then-kept")
	report(u3, "fixed-then-kept")
	must(svc.ResolveCardIssues(ctx, u1, card["fixed-then-kept"]))
	// «Всё верно» answers only the complaint after the edit: the first one
	// stays fixed even after another edit.
	edit("fixed-kept-edited")
	report(u3, "fixed-kept-edited")
	must(svc.ResolveCardIssues(ctx, u1, card["fixed-kept-edited"]))
	edit("fixed-kept-edited")
	must(svc.DeleteCard(ctx, u1, card["deleted"]))
	must(sets.DeleteSet(ctx, u1, uuid.UUID(gone.ID.Bytes)))

	rows, err := q.ListCardIssues(ctx)
	must(err)
	type issue struct{ question, status, set string }
	var got []issue
	for _, r := range rows {
		got = append(got, issue{r.Question, r.Status, r.SetTitle.String})
	}
	want := []issue{
		{"fixed-kept-edited", "kept", "Биология"},
		{"fixed-then-kept", "kept", "Биология"},
		{"set-deleted", "deleted", ""},
		{"deleted", "deleted", "Биология"},
		{"fixed-kept-edited", "fixed", "Биология"},
		{"fixed-then-kept", "fixed", "Биология"},
		{"kept-then-edited", "kept", "Биология"},
		{"kept", "kept", "Биология"},
		{"fixed", "fixed", "Биология"},
		{"open", "open", "Биология"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("issues:\n got %v\nwant %v", got, want)
	}
	last := rows[len(rows)-1]
	if uuid.UUID(last.ReporterID.Bytes) != u2 || last.ReporterFirstName != "Анна" || last.ReporterLastName.String != "Иванова" || last.Kind != "flip" || last.AnswerText.String != "A" {
		t.Errorf("row = %+v", last)
	}
	for _, r := range rows {
		if r.Question == "set-deleted" && (r.Explanation != "E" || r.SourceQuote != "Quote" || r.SetTitle.Valid) {
			t.Errorf("copy of the deleted set's card = %+v", r)
		}
	}
}
