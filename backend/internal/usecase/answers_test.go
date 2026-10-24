package usecase_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// answerFixture is one card of a reader's feed; recording an answer takes
// it out of the feed, as moving the fact's review date does.
type answerFixture struct {
	mockQuerier
	card     db.GetCardByIDRow
	inFeed   atomic.Bool
	recorded atomic.Int32
	last     atomic.Pointer[bool]
}

func newAnswerFixture(card db.GetCardByIDRow) *answerFixture {
	f := &answerFixture{card: card}
	f.inFeed.Store(true)
	f.getCardByIDFunc = func(_ context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error) {
		if arg.ID != f.card.ID || arg.UserID != (pgtype.UUID{Bytes: testUser1, Valid: true}) {
			return db.GetCardByIDRow{}, pgx.ErrNoRows
		}
		return f.card, nil
	}
	f.getFeedCardsForUserFunc = func(context.Context, pgtype.UUID) ([]db.GetFeedCardsForUserRow, error) {
		if !f.inFeed.Load() {
			return nil, nil
		}
		return []db.GetFeedCardsForUserRow{db.GetFeedCardsForUserRow(f.card)}, nil
	}
	f.getLastAnswerFunc = func(context.Context, db.GetLastAnswerParams) (bool, error) {
		if v := f.last.Load(); v != nil {
			return *v, nil
		}
		return false, pgx.ErrNoRows
	}
	f.recordAnswerFunc = func(_ context.Context, arg db.RecordAnswerParams) (db.Answer, error) {
		f.recorded.Add(1)
		correct := arg.Correct
		f.last.Store(&correct)
		f.inFeed.Store(false)
		return db.Answer{}, nil
	}
	return f
}

func testCard(kind string, answer string) db.GetCardByIDRow {
	return db.GetCardByIDRow{
		ID:          pgtype.UUID{Bytes: uuid.New(), Valid: true},
		FactID:      pgtype.UUID{Bytes: testFact1, Valid: true},
		Kind:        db.CardKind(kind),
		Question:    "На каком массиве применим бинарный поиск?",
		AnswerText:  pgtype.Text{String: answer, Valid: true},
		Explanation: "Иначе половину не отбросить.",
		SourceQuote: "Бинарный поиск работает только на отсортированном массиве",
	}
}

func TestAnswerQuestionGradesEachKind(t *testing.T) {
	for _, tc := range []struct {
		name, kind, stored, given string
		want                      bool
	}{
		{"flip knew", "flip", "отсортированный", "true", true},
		{"flip did not know", "flip", "отсортированный", "false", false},
		{"boolean right", "boolean", "false", "false", true},
		{"boolean wrong", "boolean", "false", "true", false},
		{"choice right", "choice", `["A","B"]`, `["B","A"]`, true},
		{"choice partial", "choice", `["A","B"]`, `["A"]`, false},
		{"input same words", "input", "на отсортированном массиве", "Отсортированный массив.", true},
		{"input other answer", "input", "на отсортированном массиве", "на любом", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAnswerFixture(testCard(tc.kind, tc.stored))
			svc := usecase.NewCardService(f, usecase.NewUserService(f))
			res, err := svc.AnswerQuestion(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes), tc.given)
			if err != nil || res.IsCorrect != tc.want || res.Method != contracts.AnswerQuestionResponseMethodLocal {
				t.Fatalf("AnswerQuestion() = %+v, %v; want correct=%v", res, err, tc.want)
			}
			if f.recorded.Load() != 1 {
				t.Fatalf("recorded %d answers, want 1", f.recorded.Load())
			}
			if !res.Reveal.Set || res.Reveal.Value.SourceQuote != f.card.SourceQuote || res.Reveal.Value.Explanation != f.card.Explanation {
				t.Fatalf("the answer must come with what the feed hid: %+v", res.Reveal)
			}
		})
	}
}

func TestAnswerQuestionRejectsMalformedAnswers(t *testing.T) {
	for _, tc := range []struct{ kind, stored, given string }{
		{"flip", "ответ", "знал"},
		{"boolean", "true", "верно"},
		{"table", "", "X:x"},
	} {
		f := newAnswerFixture(testCard(tc.kind, tc.stored))
		svc := usecase.NewCardService(f, usecase.NewUserService(f))
		if _, err := svc.AnswerQuestion(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes), tc.given); !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("%s %q: err = %v, want ErrValidation", tc.kind, tc.given, err)
		}
		if f.recorded.Load() != 0 {
			t.Fatalf("%s: a malformed answer was recorded", tc.kind)
		}
	}
}

func TestAnswerQuestionGradesTableByPlaces(t *testing.T) {
	card := testCard("table", "")
	columns := func(context.Context, pgtype.UUID) ([]db.CardTableColumn, error) {
		return []db.CardTableColumn{{Name: "X"}, {Name: "Y"}}, nil
	}
	items := func(context.Context, pgtype.UUID) ([]db.CardTableItem, error) {
		return []db.CardTableItem{{ItemText: "x", ColumnName: "X"}, {ItemText: "y", ColumnName: "Y"}}, nil
	}
	for _, tc := range []struct {
		given string
		want  bool
	}{
		{`[{"text":"y","column":"Y"},{"text":"x","column":"X"}]`, true},
		{`[{"text":"x","column":"Y"},{"text":"y","column":"X"}]`, false},
		{`[{"text":"x","column":"X"}]`, false},
		{`[{"text":"x","column":"X"},{"text":"x","column":"X"}]`, false},
		{`[{"text":"x","column":"X"},{"text":"y","column":"Y"},{"text":"z","column":"X"}]`, false},
	} {
		f := newAnswerFixture(card)
		f.getCardTableColumnsFunc = columns
		f.getCardTableItemsFunc = items
		svc := usecase.NewCardService(f, usecase.NewUserService(f))
		res, err := svc.AnswerQuestion(context.Background(), testUser1, uuid.UUID(card.ID.Bytes), tc.given)
		if err != nil || res.IsCorrect != tc.want {
			t.Fatalf("%s: %+v, %v; want correct=%v", tc.given, res, err, tc.want)
		}
		layout := res.Reveal.Value.Answer.TableLayout
		if len(layout.Items) != 2 || layout.Items[0].Column != "X" {
			t.Fatalf("the reveal must place the terms: %+v", layout)
		}
	}
}

// A card the feed does not ask now is not graded: neither a card of a
// fact that is not due nor an easier card of a due fact.
func TestAnswerQuestionOnlyGradesTheFeedCard(t *testing.T) {
	f := newAnswerFixture(testCard("flip", "отсортированный"))
	f.inFeed.Store(false)
	svc := usecase.NewCardService(f, usecase.NewUserService(f))
	_, err := svc.AnswerQuestion(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes), "true")
	if !errors.Is(err, usecase.ErrNotFound) || f.recorded.Load() != 0 {
		t.Fatalf("err = %v, recorded %d; want ErrNotFound and nothing recorded", err, f.recorded.Load())
	}

	if _, err := svc.AnswerQuestion(context.Background(), testUser2, uuid.UUID(f.card.ID.Bytes), "true"); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("someone else's card: err = %v, want ErrNotFound", err)
	}
}

// Asked again after the answer was recorded — a lost response retried, or
// a second try at a wrong answer — the endpoint repeats the verdict.
func TestAnswerQuestionRepeatsTheRecordedVerdict(t *testing.T) {
	f := newAnswerFixture(testCard("choice", `["A","B"]`))
	svc := usecase.NewCardService(f, usecase.NewUserService(f))
	id := uuid.UUID(f.card.ID.Bytes)

	first, err := svc.AnswerQuestion(context.Background(), testUser1, id, `["A"]`)
	if err != nil || first.IsCorrect {
		t.Fatalf("first answer = %+v, %v; want wrong", first, err)
	}
	again, err := svc.AnswerQuestion(context.Background(), testUser1, id, `["A","B"]`)
	if err != nil || again.IsCorrect || !again.Reveal.Set {
		t.Fatalf("second answer = %+v, %v; want the recorded wrong verdict with the reveal", again, err)
	}
	if f.recorded.Load() != 1 {
		t.Fatalf("recorded %d answers, want 1", f.recorded.Load())
	}
}

type slowModel struct{ delay time.Duration }

func (m slowModel) Complete(ctx context.Context, _ providers.Request) (providers.Response, error) {
	select {
	case <-time.After(m.delay):
		return providers.Response{Content: []byte(`{"correct": true, "reason": "По смыслу то же."}`)}, nil
	case <-ctx.Done():
		return providers.Response{}, ctx.Err()
	}
}

// Overlapping answers to one card — a double tap, or a retry while the
// model still judges the first — are recorded once.
func TestAnswerQuestionRecordsOverlappingAnswersOnce(t *testing.T) {
	f := newAnswerFixture(testCard("input", "на отсортированном массиве"))
	model := grader.Grader{Model: slowModel{delay: 20 * time.Millisecond}, Timeout: time.Second}
	svc := usecase.NewCardService(f, usecase.NewUserService(f), usecase.WithGrader(&model))
	id := uuid.UUID(f.card.ID.Bytes)

	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.AnswerQuestion(context.Background(), testUser1, id, "упорядоченный")
			results[i] = err == nil && res.IsCorrect
		}()
	}
	wg.Wait()

	if f.recorded.Load() != 1 {
		t.Fatalf("recorded %d answers, want 1", f.recorded.Load())
	}
	for i, ok := range results {
		if !ok {
			t.Fatalf("answer %d did not get the verdict", i)
		}
	}
}

type silentModel struct{}

func (silentModel) Complete(ctx context.Context, _ providers.Request) (providers.Response, error) {
	<-ctx.Done()
	return providers.Response{}, ctx.Err()
}

// Without the model's verdict nothing is recorded or revealed, and the card
// stays in the feed for the next ask.
func TestAnswerQuestionFallbackRecordsNothing(t *testing.T) {
	f := newAnswerFixture(testCard("input", "на отсортированном массиве"))
	model := grader.Grader{Model: silentModel{}, Timeout: 10 * time.Millisecond}
	svc := usecase.NewCardService(f, usecase.NewUserService(f), usecase.WithGrader(&model))

	res, err := svc.AnswerQuestion(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes), "упорядоченный")
	if err != nil || res.Method != contracts.AnswerQuestionResponseMethodFallback {
		t.Fatalf("AnswerQuestion() = %+v, %v; want fallback", res, err)
	}
	if res.Reveal.Set || f.recorded.Load() != 0 || !f.inFeed.Load() {
		t.Fatalf("fallback revealed %v, recorded %d", res.Reveal.Set, f.recorded.Load())
	}
}

func TestRevealCard(t *testing.T) {
	t.Run("flip in the feed", func(t *testing.T) {
		f := newAnswerFixture(testCard("flip", "отсортированный"))
		svc := usecase.NewCardService(f, usecase.NewUserService(f))
		r, err := svc.RevealCard(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes))
		if err != nil || r.Answer.String != "отсортированный" || r.SourceQuote != f.card.SourceQuote {
			t.Fatalf("RevealCard() = %+v, %v", r, err)
		}
		if f.recorded.Load() != 0 {
			t.Fatal("a reveal must not record an answer")
		}
	})
	t.Run("other kinds are revealed by their answer", func(t *testing.T) {
		f := newAnswerFixture(testCard("input", "отсортированный"))
		svc := usecase.NewCardService(f, usecase.NewUserService(f))
		if _, err := svc.RevealCard(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes)); !errors.Is(err, usecase.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
	t.Run("flip out of the feed and never answered", func(t *testing.T) {
		f := newAnswerFixture(testCard("flip", "отсортированный"))
		f.inFeed.Store(false)
		svc := usecase.NewCardService(f, usecase.NewUserService(f))
		if _, err := svc.RevealCard(context.Background(), testUser1, uuid.UUID(f.card.ID.Bytes)); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestGetFeedQuestionsHidesAnswers(t *testing.T) {
	card := testCard("table", "")
	q := &mockQuerier{
		getFeedCardsForUserFunc: func(context.Context, pgtype.UUID) ([]db.GetFeedCardsForUserRow, error) {
			return []db.GetFeedCardsForUserRow{db.GetFeedCardsForUserRow(card)}, nil
		},
	}
	batches := &detailBatchQuerier{mockQuerier: *q}
	cards, err := usecase.NewHomescreenService(batches, usecase.NewUserService(batches)).GetFeedQuestions(context.Background(), testUser1)
	if err != nil || len(cards) != 1 {
		t.Fatalf("GetFeedQuestions() = %+v, %v", cards, err)
	}
	table := cards[0].Table.Value
	if len(table.Columns) != 1 || len(table.Items) != 1 || table.Items[0] != "Item" {
		t.Fatalf("the terms must come without their columns: %+v", table)
	}
}

func TestGetCardsBySetIDGivesMembersQuestionsOnly(t *testing.T) {
	setID := uuid.New()
	pgSetID := pgtype.UUID{Bytes: setID, Valid: true}
	author := pgtype.UUID{Bytes: testUser3, Valid: true}
	choice := testCard("choice", `["A"]`)
	q := &detailBatchQuerier{}
	q.getSetByIDFunc = func(context.Context, db.GetSetByIDParams) (db.GetSetByIDRow, error) {
		return db.GetSetByIDRow{ID: pgSetID, AuthorID: author}, nil
	}
	q.getCardsBySetIDFunc = func(context.Context, db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error) {
		return []db.GetCardsBySetIDRow{db.GetCardsBySetIDRow(choice)}, nil
	}
	svc := usecase.NewSetService(q, usecase.NewUserService(q))

	member, err := svc.GetCardsBySetID(context.Background(), testUser1, setID)
	if err != nil || len(member) != 1 {
		t.Fatalf("member: %+v, %v", member, err)
	}
	c := member[0]
	if c.Question != choice.Question || c.Answer.Set || c.Explanation.Set || c.SourceQuote.Set || len(c.Options) != 0 || c.Table.Set {
		t.Fatalf("a member must get only the question: %+v", c)
	}

	own, err := svc.GetCardsBySetID(context.Background(), testUser3, setID)
	if err != nil || len(own) != 1 || !own[0].Answer.Set || own[0].SourceQuote.Value != choice.SourceQuote || len(own[0].Options) != 3 {
		t.Fatalf("the author must get the whole card: %+v, %v", own, err)
	}
}

// Generated choice cards keep the right options first and table terms come
// column by column, so the feed must not send them in stored order.
func TestGetFeedQuestionsShufflesOptionsAndTerms(t *testing.T) {
	choice, table := testCard("choice", `["A"]`), testCard("table", "")
	q := shuffleDetails{&mockQuerier{
		getFeedCardsForUserFunc: func(context.Context, pgtype.UUID) ([]db.GetFeedCardsForUserRow, error) {
			return []db.GetFeedCardsForUserRow{db.GetFeedCardsForUserRow(choice), db.GetFeedCardsForUserRow(table)}, nil
		},
	}}
	svc := usecase.NewHomescreenService(q, usecase.NewUserService(q))
	firstOption, firstTerm := map[string]bool{}, map[string]bool{}
	for range 40 {
		cards, err := svc.GetFeedQuestions(context.Background(), testUser1)
		if err != nil || len(cards) != 2 || len(cards[0].Options) != 4 || len(cards[1].Table.Value.Items) != 4 {
			t.Fatalf("GetFeedQuestions() = %+v, %v", cards, err)
		}
		firstOption[cards[0].Options[0]] = true
		firstTerm[cards[1].Table.Value.Items[0]] = true
	}
	if len(firstOption) < 2 || len(firstTerm) < 2 {
		t.Fatalf("stored order kept: first options %v, first terms %v", firstOption, firstTerm)
	}
}

type shuffleDetails struct{ *mockQuerier }

func (shuffleDetails) GetCardOptionsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardOption, error) {
	var rows []db.CardOption
	for _, id := range ids {
		for i, text := range []string{"A", "B", "C", "D"} {
			rows = append(rows, db.CardOption{CardID: id, Text: text, IsCorrect: i == 0})
		}
	}
	return rows, nil
}

func (shuffleDetails) GetCardTableColumnsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardTableColumn, error) {
	var rows []db.CardTableColumn
	for _, id := range ids {
		rows = append(rows, db.CardTableColumn{CardID: id, Name: "X"}, db.CardTableColumn{CardID: id, Name: "Y"})
	}
	return rows, nil
}

func (shuffleDetails) GetCardTableItemsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardTableItem, error) {
	var rows []db.CardTableItem
	for _, id := range ids {
		for _, item := range []db.CardTableItem{{ItemText: "x1", ColumnName: "X"}, {ItemText: "x2", ColumnName: "X"}, {ItemText: "y1", ColumnName: "Y"}, {ItemText: "y2", ColumnName: "Y"}} {
			item.CardID = id
			rows = append(rows, item)
		}
	}
	return rows, nil
}
