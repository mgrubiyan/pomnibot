package repository_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

func TestGenerateSetForUser_Success(t *testing.T) {
	ctx := context.Background()
	tq := newTestQuerier()
	const userID int64 = 424242
	const title = "Конспект по физике"

	createdSet, cardCount, err := repository.GenerateSetForUser(ctx, tq, userID, title)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if createdSet == nil {
		t.Fatal("expected non-nil created set")
	}
	if createdSet.Title != title {
		t.Errorf("expected title %q, got %q", title, createdSet.Title)
	}
	if createdSet.AuthorID != userID {
		t.Errorf("expected authorID %d, got %d", userID, createdSet.AuthorID)
	}
	if len(createdSet.ShareCode) != 6 {
		t.Errorf("expected 6-char share code, got %q", createdSet.ShareCode)
	}
	if _, err := strconv.Atoi(createdSet.ShareCode); err != nil {
		t.Errorf("expected numeric share code, got %q", createdSet.ShareCode)
	}
	if cardCount == 0 {
		t.Errorf("expected cardCount > 0, got %d", cardCount)
	}

	// Verify enrollment in user_sets
	if len(tq.joinedSets) != 1 {
		t.Fatalf("expected 1 joined set record, got %d", len(tq.joinedSets))
	}
	if tq.joinedSets[0].UserID != userID {
		t.Errorf("expected user_set userID %d, got %d", userID, tq.joinedSets[0].UserID)
	}

	// Verify InitUserFactProgress
	if len(tq.initProgressCalls) != 1 {
		t.Fatalf("expected 1 initProgress call, got %d", len(tq.initProgressCalls))
	}
	if tq.initProgressCalls[0].UserID != userID {
		t.Errorf("expected progress for userID %d, got %d", userID, tq.initProgressCalls[0].UserID)
	}

	// Verify Facts were assigned valid unique UUIDs and not original hardcoded ones
	if len(tq.createdFacts) == 0 {
		t.Fatal("expected facts to be created")
	}
	for _, f := range tq.createdFacts {
		if _, err := uuid.Parse(f.ID); err != nil {
			t.Errorf("expected fact ID to be a valid UUID, got %q", f.ID)
		}
		if strings.HasPrefix(f.ID, "40d5f9") {
			t.Errorf("expected fact ID to be regenerated UUID, got hardcoded %q", f.ID)
		}
	}

	// Verify all cards reference existing fact IDs from this run
	factIDSet := make(map[string]bool)
	for _, f := range tq.createdFacts {
		factIDSet[f.ID] = true
	}
	for _, c := range tq.createdCards {
		if !factIDSet[c.FactID] {
			t.Errorf("card fact_id %q not found in created facts", c.FactID)
		}
	}
}

func TestGenerateSetForUser_ShareCodeCollisionRetry(t *testing.T) {
	ctx := context.Background()
	tq := newTestQuerier()
	attempts := 0

	tq.getSetByShareCodeFunc = func(_ context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
		attempts++
		if attempts <= 2 {
			// First 2 codes collide
			return db.GetSetByShareCodeRow{ShareCode: arg.ShareCode}, nil
		}
		// 3rd code is free
		return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
	}

	set, _, err := repository.GenerateSetForUser(ctx, tq, 101, "Test Collision")
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts to generate share code, got %d", attempts)
	}
	if len(set.ShareCode) != 6 {
		t.Errorf("expected 6-char share code, got %q", set.ShareCode)
	}
}

func TestGenerateSetForUser_UniqueFactsAcrossRuns(t *testing.T) {
	ctx := context.Background()
	tq := newTestQuerier()

	_, _, err := repository.GenerateSetForUser(ctx, tq, 1, "Set 1")
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	firstRunFacts := make(map[string]bool)
	for _, f := range tq.createdFacts {
		firstRunFacts[f.ID] = true
	}

	// Run second generation
	_, _, err = repository.GenerateSetForUser(ctx, tq, 2, "Set 2")
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}

	// Ensure second run created different fact IDs
	for _, f := range tq.createdFacts[len(firstRunFacts):] {
		if firstRunFacts[f.ID] {
			t.Errorf("fact ID %q collided with first run", f.ID)
		}
	}
}

func TestGenerateSetForUser_CreateSetError(t *testing.T) {
	ctx := context.Background()
	tq := newTestQuerier()
	tq.createSetFunc = func(_ context.Context, _ db.CreateSetParams) (db.Set, error) {
		return db.Set{}, errors.New("db error")
	}

	_, _, err := repository.GenerateSetForUser(ctx, tq, 1, "Error Set")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
