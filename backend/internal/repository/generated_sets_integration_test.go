package repository_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestGeneratedSetOrderAndChoiceIssuesPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	sets := usecase.NewSetService(q, users)
	cards := usecase.NewCardService(q, users)
	owner, reader := uuid.New(), uuid.New()
	pgReader := pgtype.UUID{Bytes: reader, Valid: true}
	result := generator.Result{Artifact: &generator.Artifact{Version: generator.PipelineVersion}}
	// More than twenty due facts must reach the feed, in generation order.
	for i := range 25 {
		base := func(kind string) generator.CardBase {
			return generator.CardBase{Question: fmt.Sprintf("%d %s", i, kind), Source: "Explanation", SourceQuote: "Quote"}
		}
		result.Facts = append(result.Facts, generator.Fact{Name: fmt.Sprintf("Fact %d", i), Pages: []int{1}, Cards: []generator.Card{
			generator.FlipCard{CardBase: base("flip"), Answer: "Recall"},
			generator.BooleanCard{CardBase: base("boolean"), Answer: false},
			generator.ChoiceCard{CardBase: base("choice"), RightAnswers: []string{"A", "B"}, WrongAnswers: []string{"C"}},
			generator.TableCard{CardBase: base("table"), Columns: []generator.Column{{Name: "X", Rows: []string{"x"}}, {Name: "Y", Rows: []string{"y"}}}},
			generator.InputCard{CardBase: base("input"), Answer: "Typed"},
		}})
	}
	set, err := sets.SaveGeneratedSet(ctx, owner, "Generated", result)
	if err != nil {
		t.Fatal(err)
	}
	if set.CardsTotal != 125 {
		t.Fatalf("saved %d cards", set.CardsTotal)
	}
	if _, err = sets.JoinSetByShareCode(ctx, reader, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	facts, err := q.GetFactsBySetID(ctx, pgtype.UUID{Bytes: set.ID, Valid: true})
	if err != nil || len(facts) != 25 {
		t.Fatalf("facts=%d error=%v", len(facts), err)
	}
	for i, fact := range facts {
		if fact.Name != result.Facts[i].Name || uuid.UUID(fact.ID.Bytes).Version() != 7 {
			t.Fatalf("fact %d: %+v", i, fact)
		}
		if i > 0 && bytes.Compare(facts[i-1].ID.Bytes[:], fact.ID.Bytes[:]) >= 0 {
			t.Fatal("fact order changed")
		}
	}
	all, err := sets.GetCardsBySetID(ctx, reader, set.ID)
	if err != nil || len(all) != 125 {
		t.Fatalf("cards=%d error=%v", len(all), err)
	}
	kinds := []contracts.CardKind{contracts.CardKindFlip, contracts.CardKindBoolean, contracts.CardKindChoice, contracts.CardKindTable, contracts.CardKindInput}
	for i, card := range all {
		if card.Kind != kinds[i%5] || card.FactId != uuid.UUID(facts[i/5].ID.Bytes) || card.Question != fmt.Sprintf("%d %s", i/5, kinds[i%5]) || card.ID.Version() != 7 || card.SourceQuote != "Quote" {
			t.Fatalf("card %d: %+v", i, card)
		}
		if i > 0 && bytes.Compare(all[i-1].ID[:], card.ID[:]) >= 0 {
			t.Fatal("card order changed")
		}
	}
	if !slices.Equal(all[2].Answer.StringArray, []string{"A", "B"}) || !slices.Equal(all[2].Options, []string{"A", "B", "C"}) || !slices.Equal(all[3].Table.Value.Columns, []string{"X", "Y"}) || all[3].Table.Value.Items[1].Text != "y" {
		t.Fatal("choice/table content lost")
	}
	setStep := func(step int) {
		t.Helper()
		if _, err := pool.Exec(ctx, "UPDATE user_fact_progress SET step=$2, next_review_at='1970-01-01' WHERE user_id=$1", reader, step); err != nil {
			t.Fatal(err)
		}
	}
	assertFeed := func(user uuid.UUID, kind contracts.CardKind, count int) {
		t.Helper()
		feed, err := q.GetFeedCardsForUser(ctx, pgtype.UUID{Bytes: user, Valid: true})
		if err != nil || len(feed) != count {
			t.Fatalf("feed=%d want=%d error=%v", len(feed), count, err)
		}
		for i, card := range feed {
			if card.Kind != db.CardKind(kind) || card.FactID != facts[i].ID {
				t.Fatalf("feed card %d: %+v", i, card)
			}
		}
	}
	for step := range 7 {
		setStep(step)
		assertFeed(reader, kinds[min(step, 4)], 25)
	}
	setStep(2)
	choiceID := all[2].ID
	if err = cards.ReportCardIssue(ctx, reader, choiceID, contracts.CardIssueReasonAnswer); err != nil {
		t.Fatal(err)
	}
	feed, err := q.GetFeedCardsForUser(ctx, pgReader)
	if err != nil || len(feed) != 25 || feed[0].Kind != db.CardKindBoolean {
		t.Fatalf("reported feed: %v %v", feed, err)
	}
	// An outstanding complaint blocks the reported card and all harder ones.
	setStep(4)
	feed, err = q.GetFeedCardsForUser(ctx, pgReader)
	if err != nil || len(feed) != 25 || feed[0].Kind != db.CardKindBoolean {
		t.Fatalf("harder card exposed: %v %v", feed, err)
	}
	assertFeed(owner, contracts.CardKindFlip, 25)
	updated, err := cards.UpdateCard(ctx, owner, choiceID, &contracts.UpdateCardRequest{Answer: contracts.NewOptCardAnswer(contracts.NewStringArrayCardAnswer([]string{"B", "C"}))})
	if err != nil || !slices.Equal(updated.Answer.StringArray, []string{"B", "C"}) {
		t.Fatalf("edit: %+v %v", updated, err)
	}
	setStep(2)
	assertFeed(reader, contracts.CardKindChoice, 25)
	roundTrip, err := sets.GetCardsBySetID(ctx, reader, set.ID)
	if err != nil || !slices.Equal(roundTrip[2].Answer.StringArray, []string{"B", "C"}) {
		t.Fatalf("edited answer: %v", err)
	}
	options, err := q.GetCardOptions(ctx, pgtype.UUID{Bytes: choiceID, Valid: true})
	if err != nil || len(options) != 3 || options[0].IsCorrect || !options[1].IsCorrect || !options[2].IsCorrect {
		t.Fatalf("edited options: %+v %v", options, err)
	}
	for _, tc := range []struct {
		answer  string
		correct bool
	}{{`["C","B"]`, true}, {`["B"]`, false}, {`["A","B"]`, false}} {
		verdict, err := cards.CheckAnswer(ctx, reader, choiceID, tc.answer)
		if err != nil || verdict.IsCorrect != tc.correct {
			t.Fatalf("check %s: %+v %v", tc.answer, verdict, err)
		}
	}
	answer, err := cards.AnswerQuestion(ctx, reader, choiceID, `["C","B"]`)
	if err != nil || !answer.IsCorrect {
		t.Fatalf("answer: %+v %v", answer, err)
	}
	progress, err := q.GetUserFactProgress(ctx, db.GetUserFactProgressParams{UserID: pgReader, FactID: facts[0].ID})
	if err != nil || progress.Step != 3 {
		t.Fatalf("progress: %+v %v", progress, err)
	}
	// Keeping the card also restores it, without requiring another edit.
	setStep(2)
	if err = cards.ReportCardIssue(ctx, reader, choiceID, contracts.CardIssueReasonWording); err != nil {
		t.Fatal(err)
	}
	if err = cards.ResolveCardIssues(ctx, owner, choiceID); err != nil {
		t.Fatal(err)
	}
	setStep(2)
	assertFeed(reader, contracts.CardKindChoice, 25)
	issues, err := cards.GetSetIssues(ctx, owner, set.ID)
	if err != nil || len(issues) != 0 {
		t.Fatalf("open issues: %+v %v", issues, err)
	}
}
