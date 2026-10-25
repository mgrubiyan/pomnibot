package repository_test

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestGeneratedSetOrderAndChoiceIssuesPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	sets := usecase.NewSetService(q, users)
	cards := usecase.NewCardService(q, users, usecase.WithTransactions(repository.NewManualStore(pool)))
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
	set, err := sets.SaveGeneratedSet(ctx, owner, "Generated", result, nil)
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
	all, err := sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || len(all) != 125 {
		t.Fatalf("cards=%d error=%v", len(all), err)
	}
	// A member cannot view the cards list; only the author can.
	if _, err := sets.GetCardsBySetID(ctx, reader, set.ID); !errors.Is(err, usecase.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for member, got: %v", err)
	}
	kinds := []contracts.CardKind{contracts.CardKindFlip, contracts.CardKindBoolean, contracts.CardKindChoice, contracts.CardKindTable, contracts.CardKindInput}
	for i, card := range all {
		if card.Kind != kinds[i%5] || card.FactId != uuid.UUID(facts[i/5].ID.Bytes) || card.Question != fmt.Sprintf("%d %s", i/5, kinds[i%5]) || card.ID.Version() != 7 || card.SourceQuote.Value != "Quote" {
			t.Fatalf("card %d: %+v", i, card)
		}
		if i > 0 && bytes.Compare(all[i-1].ID[:], card.ID[:]) >= 0 {
			t.Fatal("card order changed")
		}
	}
	if !slices.Equal(all[2].Answer.Value.StringArray, []string{"A", "B"}) || !slices.Equal(all[2].Options, []string{"A", "B", "C"}) || !slices.Equal(all[3].Table.Value.Columns, []string{"X", "Y"}) || all[3].Table.Value.Items[1].Text != "y" {
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
	if err != nil || !slices.Equal(updated.Answer.Value.StringArray, []string{"B", "C"}) {
		t.Fatalf("edit: %+v %v", updated, err)
	}
	setStep(2)
	assertFeed(reader, contracts.CardKindChoice, 25)
	roundTrip, err := sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || !slices.Equal(roundTrip[2].Answer.Value.StringArray, []string{"B", "C"}) {
		t.Fatalf("edited answer: %v", err)
	}
	options, err := q.GetCardOptions(ctx, pgtype.UUID{Bytes: choiceID, Valid: true})
	if err != nil || len(options) != 3 || options[0].IsCorrect || !options[1].IsCorrect || !options[2].IsCorrect {
		t.Fatalf("edited options: %+v %v", options, err)
	}
	// Only the card the feed asks now is graded, not an easier or a harder
	// card of the same fact.
	for _, other := range []uuid.UUID{all[0].ID, all[4].ID} {
		if _, err := cards.AnswerQuestion(ctx, reader, other, "true", uuid.New()); !errors.Is(err, usecase.ErrConflict) {
			t.Fatalf("answer to a card out of the feed: %v", err)
		}
	}
	if _, err := cards.RevealCard(ctx, reader, all[0].ID); !errors.Is(err, usecase.ErrConflict) {
		t.Fatalf("reveal of a flip out of the feed: %v", err)
	}
	if _, err := cards.RevealCard(ctx, reader, choiceID); !errors.Is(err, usecase.ErrValidation) {
		t.Fatalf("reveal of a choice card: %v", err)
	}
	beforeAnswer := uuid.Must(uuid.NewV7())
	time.Sleep(5 * time.Millisecond)
	attemptID := uuid.New()
	answer, err := cards.AnswerQuestion(ctx, reader, choiceID, `["C","B"]`, attemptID)
	if err != nil || !answer.IsCorrect || !slices.Equal(answer.Reveal.Value.Answer.StringArray, []string{"B", "C"}) {
		t.Fatalf("answer: %+v %v", answer, err)
	}
	// Asked again, the endpoint repeats the recorded verdict and records nothing.
	again, err := cards.AnswerQuestion(ctx, reader, choiceID, `["B"]`, attemptID)
	if err != nil || !again.IsCorrect {
		t.Fatalf("second answer: %+v %v", again, err)
	}
	var recorded int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM answers WHERE user_id=$1 AND card_id=$2", reader, choiceID).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("recorded answers=%d error=%v", recorded, err)
	}
	// Only a verdict recorded after the bound is repeated.
	time.Sleep(5 * time.Millisecond)
	afterAnswer := uuid.Must(uuid.NewV7())
	lastSince := func(bound uuid.UUID) error {
		_, err := q.GetLastAnswer(ctx, db.GetLastAnswerParams{
			UserID:  pgReader,
			CardID:  pgtype.UUID{Bytes: choiceID, Valid: true},
			SinceID: pgtype.UUID{Bytes: bound, Valid: true},
		})
		return err
	}
	if err := lastSince(beforeAnswer); err != nil {
		t.Fatalf("the verdict recorded after the bound: %v", err)
	}
	if err := lastSince(afterAnswer); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an older verdict was repeated: %v", err)
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

func TestSetWarningsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	q := db.New(pool)
	sets := usecase.NewSetService(q, usecase.NewUserService(q))
	owner, reader := uuid.New(), uuid.New()
	warnings := []string{"Страница 2 размыта", "Пропущен слайд", "Не описана функция", "Пропущена формула"}
	set, err := sets.SaveGeneratedSet(t.Context(), owner, "Лекция", generator.Result{}, warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(set.Warnings, warnings) {
		t.Fatalf("saved warnings = %q", set.Warnings)
	}
	joined, err := sets.JoinSetByShareCode(t.Context(), reader, set.ShareCode.Value)
	if err != nil || len(joined.Warnings) != 0 {
		t.Fatalf("join exposed warnings: %+v, err=%v", joined, err)
	}
	for _, user := range []uuid.UUID{owner, reader} {
		want := warnings
		if user != owner {
			want = nil
		}
		row, err := q.GetSetByID(t.Context(), db.GetSetByIDParams{ID: pgtype.UUID{Bytes: set.ID, Valid: true}, UserID: pgtype.UUID{Bytes: user, Valid: true}})
		if err != nil || !slices.Equal(row.Warnings, want) {
			t.Fatalf("SQL warnings for %s = %q, err=%v", user, row.Warnings, err)
		}
		loaded, err := sets.GetSet(t.Context(), user, set.ID)
		if err != nil || !slices.Equal(loaded.Warnings, want) {
			t.Fatalf("API set for %s = %+v, err=%v", user, loaded, err)
		}
	}
}

// A one-card fact remains eligible for early review after reaching its last step.
func TestEarlyReviewAnswerAttemptsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	store := repository.NewManualStore(pool)
	sets := usecase.NewSetService(q, users, store)
	owner, reader := uuid.New(), uuid.New()
	set, err := sets.CreateSet(ctx, owner, &contracts.CreateSetRequest{
		Title: "Early review",
		Card: contracts.NewOptCreateCardRequest(contracts.CreateCardRequest{
			Kind: contracts.CardKindBoolean, Question: "True?", Answer: contracts.NewBoolCardAnswer(true),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sets.JoinSetByShareCode(ctx, reader, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	feed, err := q.GetSetFeedCardsForUser(ctx, db.GetSetFeedCardsForUserParams{
		UserID: pgtype.UUID{Bytes: reader, Valid: true}, SetID: pgtype.UUID{Bytes: set.ID, Valid: true},
	})
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed=%+v error=%v", feed, err)
	}
	card := feed[0]
	firstAttempt, secondAttempt := uuid.New(), uuid.New()
	// Rebuild the service too: deduplication must survive a server restart.
	answer := func(given string, attemptID uuid.UUID, want bool) {
		t.Helper()
		svc := usecase.NewCardService(q, users, usecase.WithTransactions(store))
		res, err := svc.AnswerQuestion(ctx, reader, uuid.UUID(card.ID.Bytes), given, attemptID)
		if err != nil || res.IsCorrect != want {
			t.Fatalf("answer=%+v error=%v, want %v", res, err, want)
		}
	}
	answer("true", firstAttempt, true)
	answer("false", secondAttempt, false)
	// A delayed retry still gets its own verdict after another review.
	answer("true", firstAttempt, true)
	answer("false", secondAttempt, false)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM answers WHERE user_id=$1 AND card_id=$2", reader, card.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("answers=%d error=%v, want 2", count, err)
	}
	progress, err := q.GetUserFactProgress(ctx, db.GetUserFactProgressParams{
		UserID: pgtype.UUID{Bytes: reader, Valid: true}, FactID: card.FactID,
	})
	if err != nil || progress.Reps != 2 || progress.Step != 0 {
		t.Fatalf("progress=%+v error=%v, want two reviews and step 0", progress, err)
	}
}
