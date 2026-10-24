package repository_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestIllustrationsRoundTripAndEditPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	sets := usecase.NewSetService(q, users)
	service := usecase.NewCardService(q, users, usecase.WithManualCards(repository.NewManualStore(pool)))
	home := usecase.NewHomescreenService(q, users)
	owner := uuid.New()
	question, explanation := `<svg viewBox="0 0 1 1"/>`, `<svg viewBox="0 0 2 2"/>`
	base := generator.CardBase{Question: `**Формула** $x^2$`, Source: "| x | y |\n|---|---|\n| 2 | 4 |", SourceQuote: "  x² = 4\n", QuestionSvg: question, ExplanationSvg: explanation}
	set, err := sets.SaveGeneratedSet(ctx, owner, "Math", generator.Result{Facts: []generator.Fact{{Name: "Math", Pages: []int{1}, Cards: []generator.Card{generator.FlipCard{CardBase: base, Answer: `$$\frac{4}{2}$$`}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("cards: %v %v", all, err)
	}
	assertSVG := func(c contracts.Card) {
		t.Helper()
		if c.QuestionSvg.Value != question || !c.QuestionSvg.Set || c.ExplanationSvg.Value != explanation || !c.ExplanationSvg.Set || c.Question != base.Question || c.SourceQuote.Value != base.SourceQuote || c.Explanation.Value != base.Source {
			t.Fatalf("lost content: %+v", c)
		}
		data, err := json.Marshal(&c)
		var wire map[string]any
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &wire); err != nil || wire["questionSvg"] != question || wire["explanationSvg"] != explanation || wire["answer"] != `$$\frac{4}{2}$$` {
			t.Fatalf("API serialization: %s %v", data, err)
		}
	}
	assertSVG(all[0])
	feed, err := home.GetFeedQuestions(ctx, owner)
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed: %v %v", feed, err)
	}
	if feed[0].QuestionSvg.Value != question {
		t.Fatalf("feed lost question SVG: %+v", feed[0])
	}
	feedData, err := json.Marshal(&feed[0])
	if err != nil {
		t.Fatal(err)
	}
	var feedWire map[string]any
	if err = json.Unmarshal(feedData, &feedWire); err != nil {
		t.Fatal(err)
	}
	if _, leaked := feedWire["explanationSvg"]; leaked {
		t.Fatal("feed exposed the explanation image before the answer")
	}
	reveal, err := service.RevealCard(ctx, owner, all[0].ID)
	if err != nil || reveal.ExplanationSvg.Value != explanation {
		t.Fatalf("reveal lost explanation SVG: %+v %v", reveal, err)
	}
	reader := uuid.New()
	if _, err = sets.JoinSetByShareCode(ctx, reader, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	memberCards, err := sets.GetCardsBySetID(ctx, reader, set.ID)
	if err != nil || len(memberCards) != 1 || memberCards[0].QuestionSvg.Value != question || memberCards[0].ExplanationSvg.Set {
		t.Fatalf("member images: %+v %v", memberCards, err)
	}
	stored, err := q.GetCardByID(ctx, db.GetCardByIDParams{ID: pgtype.UUID{Bytes: all[0].ID, Valid: true}, UserID: pgtype.UUID{Bytes: owner, Valid: true}})
	if err != nil || stored.QuestionSvg.String != question || stored.ExplanationSvg.String != explanation || stored.AnswerText.String != `$$\frac{4}{2}$$` {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	updated, err := service.UpdateCard(ctx, owner, all[0].ID, &contracts.UpdateCardRequest{Question: contracts.NewOptString("New $x$")})
	if err != nil || updated.QuestionSvg.Set || updated.ExplanationSvg.Set {
		t.Fatalf("edit retained SVG: %+v %v", updated, err)
	}
	all, err = sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || all[0].QuestionSvg.Set || all[0].ExplanationSvg.Set {
		t.Fatalf("stale SVG persisted: %+v %v", all, err)
	}
	// Old cards with NULL images remain readable and omit optional fields.
	manual, err := service.CreateCard(ctx, owner, set.ID, &contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Old", Answer: contracts.NewStringCardAnswer("A")})
	if err != nil || manual.QuestionSvg.Set || manual.ExplanationSvg.Set {
		t.Fatalf("old card: %+v %v", manual, err)
	}
	data, err := json.Marshal(manual)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"questionSvg", "explanationSvg"} {
		if _, found := wire[name]; found {
			t.Fatalf("absent image serialized: %s", data)
		}
	}
}
