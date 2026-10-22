package repository_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type testQuerier struct {
	reports []db.SaveProcessingReportParams
	db.Querier
	mu                sync.Mutex
	createdFacts      []db.CreateFactParams
	createdCards      []db.CreateCardParams
	createdOptions    []db.CreateCardOptionParams
	createdColumns    []db.CreateCardTableColumnParams
	createdItems      []db.CreateCardTableItemParams
	initProgressCalls []db.InitUserFactProgressParams
}

func newTestQuerier() *testQuerier {
	return &testQuerier{}
}

func (q *testQuerier) GetSetByShareCode(context.Context, db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
	return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
}

func (q *testQuerier) CreateSet(_ context.Context, arg db.CreateSetParams) (db.Set, error) {
	var setUUID pgtype.UUID
	_ = setUUID.Scan(uuid.New().String())
	return db.Set{
		ID:        setUUID,
		Title:     arg.Title,
		AuthorID:  arg.AuthorID,
		ShareCode: arg.ShareCode,
	}, nil
}

func (q *testQuerier) JoinSet(_ context.Context, arg db.JoinSetParams) (db.UserSet, error) {
	return db.UserSet{
		UserID: arg.UserID,
		SetID:  arg.SetID,
	}, nil
}

func (q *testQuerier) UpsertTopic(_ context.Context, name string) (db.Topic, error) {
	var topicUUID pgtype.UUID
	_ = topicUUID.Scan(uuid.New().String())
	return db.Topic{
		ID:   topicUUID,
		Name: name,
	}, nil
}

func (q *testQuerier) CreateFact(_ context.Context, arg db.CreateFactParams) (db.Fact, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createdFacts = append(q.createdFacts, arg)
	return db.Fact{
		ID:      arg.ID,
		SetID:   arg.SetID,
		TopicID: arg.TopicID,
		Name:    arg.Name,
	}, nil
}

func (q *testQuerier) CreateCard(_ context.Context, arg db.CreateCardParams) (db.Card, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createdCards = append(q.createdCards, arg)
	return db.Card{
		ID:          arg.ID,
		FactID:      arg.FactID,
		Kind:        arg.Kind,
		Question:    arg.Question,
		AnswerText:  arg.AnswerText,
		Explanation: arg.Explanation,
		SourceQuote: arg.SourceQuote,
		SourceRef:   arg.SourceRef,
	}, nil
}

func (q *testQuerier) CreateCardOption(_ context.Context, arg db.CreateCardOptionParams) (db.CardOption, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createdOptions = append(q.createdOptions, arg)
	return db.CardOption{}, nil
}

func (q *testQuerier) CreateCardTableColumn(_ context.Context, arg db.CreateCardTableColumnParams) (db.CardTableColumn, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createdColumns = append(q.createdColumns, arg)
	return db.CardTableColumn{}, nil
}

func (q *testQuerier) CreateCardTableItem(_ context.Context, arg db.CreateCardTableItemParams) (db.CardTableItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.createdItems = append(q.createdItems, arg)
	return db.CardTableItem{}, nil
}

func (q *testQuerier) InitUserFactProgress(_ context.Context, arg db.InitUserFactProgressParams) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.initProgressCalls = append(q.initProgressCalls, arg)
	return nil
}

func TestSaveGeneratedSet_Success(t *testing.T) {
	ctx := context.Background()
	tq := newTestQuerier()
	const userID int64 = 999
	const title = "Generated Set"

	genResult := generator.Result{
		Facts: []cards.Fact{
			{ID: "fact-1", Name: "Fact One", Topic: "Biology"},
			{ID: "fact-2", Name: "Fact Two", Topic: "Biology"},
		},
		Cards: []cards.Card{
			{
				FactID:      "fact-1",
				Kind:        cards.KindChoice,
				Question:    "What is fact one?",
				Options:     []string{"A", "B", "C"},
				Answer:      "A",
				Explanation: "Because A",
				SourceQuote: "Quote 1",
				SourceRef:   "Page 1",
			},
			{
				FactID:      "fact-2",
				Kind:        cards.KindFlip,
				Question:    "What is fact two?",
				Answer:      "Answer 2",
				Explanation: "Because 2",
				SourceQuote: "Quote 2",
				SourceRef:   "Page 2",
			},
		},
	}

	set, cardCount, err := repository.SaveGeneratedSet(ctx, tq, userID, title, genResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if set.Title != title {
		t.Errorf("expected title %q, got %q", title, set.Title)
	}
	if set.AuthorID != userID {
		t.Errorf("expected authorID %d, got %d", userID, set.AuthorID)
	}
	if cardCount != 2 {
		t.Errorf("expected 2 cards, got %d", cardCount)
	}
	if len(tq.createdFacts) != 2 {
		t.Errorf("expected 2 created facts, got %d", len(tq.createdFacts))
	}
	if len(tq.createdCards) != 2 {
		t.Errorf("expected 2 created cards, got %d", len(tq.createdCards))
	}
	if len(tq.createdOptions) != 3 {
		t.Errorf("expected 3 options for choice card, got %d", len(tq.createdOptions))
	}
}

func TestSaveGeneratedSet_TableCard(t *testing.T) {
	tq := newTestQuerier()
	genResult := generator.Result{
		Facts: []cards.Fact{{ID: "fact-1", Name: "Прокариоты и эукариоты", Topic: "Клетка"}},
		Cards: []cards.Card{{
			FactID:   "fact-1",
			Kind:     cards.KindTable,
			Question: "Распределите организмы по строению клетки",
			Table: &cards.Table{
				Columns: []string{"Прокариоты", "Эукариоты"},
				Items: []cards.TableItem{
					{Text: "грибы", Column: "Эукариоты"}, {Text: "бактерии", Column: "Прокариоты"},
					{Text: "археи", Column: "Прокариоты"}, {Text: "растения", Column: "Эукариоты"},
				},
			},
			SourceQuote: "К прокариотам относятся бактерии и археи, к эукариотам — растения и грибы.",
		}},
	}

	if _, n, err := repository.SaveGeneratedSet(context.Background(), tq, 1, "Клетка", genResult); err != nil || n != 1 {
		t.Fatalf("SaveGeneratedSet = %d cards, err %v", n, err)
	}
	if len(tq.createdColumns) != 2 || tq.createdColumns[1].Name != "Эукариоты" || tq.createdColumns[1].Position != 1 {
		t.Errorf("columns = %+v", tq.createdColumns)
	}
	if len(tq.createdItems) != 4 || tq.createdItems[0].ItemText != "грибы" || tq.createdItems[0].ColumnName != "Эукариоты" {
		t.Errorf("items = %+v, want all four in the order given", tq.createdItems)
	}
	for i, item := range tq.createdItems {
		if item.Position != int32(i) {
			t.Errorf("item %d position = %d: the feed must show them in the order given", i, item.Position)
		}
	}
	if tq.createdCards[0].AnswerText.Valid {
		t.Errorf("answer = %+v, want none: the layout is the answer", tq.createdCards[0].AnswerText)
	}
}

func (q *testQuerier) SaveProcessingReport(_ context.Context, arg db.SaveProcessingReportParams) (int64, error) {
	q.reports = append(q.reports, arg)
	return 1, nil
}
func TestSaveGeneratedSetPreservesPrivateSourceArtifact(t *testing.T) {
	q := newTestQuerier()
	result := generator.Result{Artifact: &generator.Artifact{Version: generator.PipelineVersion, Files: []ingest.File{{Name: "private.txt", Data: []byte("source")}}}, Cards: []cards.Card{{FactID: "f", Evidence: []material.Evidence{{BlockID: "p1-b1", Quote: "source"}}}}}
	_, _, err := repository.SaveGeneratedSet(context.Background(), q, 42, "notes", result)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.reports) != 1 || q.reports[0].AuthorID != 42 {
		t.Fatal("private artifact not saved for owner")
	}
	var restored generator.Result
	if err := json.Unmarshal(q.reports[0].Report, &restored); err != nil {
		t.Fatal(err)
	}
	if string(restored.Artifact.Files[0].Data) != "source" || restored.Cards[0].Evidence[0].Quote != "source" {
		t.Fatal("lost evidence or original")
	}
}

// Offline verification of an actual generation artifact; never contacts a model.
func TestSaveRecordedGeneration(t *testing.T) {
	path := os.Getenv("POMNIBOT_SAVED_GENERATION")
	if path == "" {
		t.Skip("private saved generation required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r generator.Result
	if err = json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Cards) == 0 || len(r.Facts) == 0 {
		t.Fatal("empty recording")
	}
	r.Artifact = &generator.Artifact{Version: generator.PipelineVersion}
	q := newTestQuerier()
	_, count, err := repository.SaveGeneratedSet(context.Background(), q, 42, "Recorded lecture", r)
	if err != nil || count != len(r.Cards) || len(q.createdFacts) != len(r.Facts) || len(q.initProgressCalls) != 1 || len(q.reports) != 1 {
		t.Fatalf("save lost cards, facts, progress or private report: count=%d err=%v", count, err)
	}
	for i, c := range r.Cards {
		got := q.createdCards[i]
		if got.Question != c.Question || got.AnswerText.String != c.Answer || got.SourceQuote != c.SourceQuote || got.SourceRef.String != c.SourceRef || got.Kind != string(c.Kind) {
			t.Fatalf("card %d changed in storage adapter", i+1)
		}
	}
	var restored generator.Result
	if err = json.Unmarshal(q.reports[0].Report, &restored); err != nil || len(restored.Cards) != len(r.Cards) || restored.Budget != r.Budget {
		t.Fatalf("private report round trip failed: %v", err)
	}
	t.Logf("saved %d recorded cards, %d facts and bulk progress initialization through repository adapter; database is mocked", count, len(r.Facts))
}
