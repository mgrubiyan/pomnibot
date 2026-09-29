package repository_test

import (
	"context"
	_ "embed"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type testQuerier struct {
	db.Querier
	mu                    sync.Mutex
	setsByShareCode       map[string]db.GetSetByShareCodeRow
	createdSets           []db.CreateSetParams
	joinedSets            []db.JoinSetParams
	createdFacts          []db.CreateFactParams
	createdCards          []db.CreateCardParams
	createdOptions        []db.CreateCardOptionParams
	createdColumns        []db.CreateCardTableColumnParams
	createdItems          []db.CreateCardTableItemParams
	initProgressCalls     []db.InitUserFactProgressParams
	upsertTopicCalls      []string
	getSetByShareCodeFunc func(ctx context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error)
	createSetFunc         func(ctx context.Context, arg db.CreateSetParams) (db.Set, error)
}

func newTestQuerier() *testQuerier {
	return &testQuerier{
		setsByShareCode: make(map[string]db.GetSetByShareCodeRow),
	}
}

func (q *testQuerier) GetSetByShareCode(ctx context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.getSetByShareCodeFunc != nil {
		return q.getSetByShareCodeFunc(ctx, arg)
	}
	if row, ok := q.setsByShareCode[arg.ShareCode]; ok {
		return row, nil
	}
	return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
}

func (q *testQuerier) CreateSet(ctx context.Context, arg db.CreateSetParams) (db.Set, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.createSetFunc != nil {
		return q.createSetFunc(ctx, arg)
	}
	q.createdSets = append(q.createdSets, arg)
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
	q.mu.Lock()
	defer q.mu.Unlock()
	q.joinedSets = append(q.joinedSets, arg)
	return db.UserSet{
		UserID: arg.UserID,
		SetID:  arg.SetID,
	}, nil
}

func (q *testQuerier) UpsertTopic(_ context.Context, name string) (db.Topic, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.upsertTopicCalls = append(q.upsertTopicCalls, name)
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
