package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestSetService_GetSet(t *testing.T) {
	setUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(setUUID.String())

	mock := &mockQuerier{
		getSetByIDFunc: func(_ context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error) {
			if arg.ID == pgUUID {
				return db.GetSetByIDRow{
					ID:              pgUUID,
					Title:           "Test Set",
					AuthorID:        100,
					AuthorFirstName: "Alice",
					ShareCode:       "123456",
					CardsTotal:      10,
					CardsDue:        5,
				}, nil
			}
			return db.GetSetByIDRow{}, pgx.ErrNoRows
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	// Success
	res, err := svc.GetSet(context.Background(), 100, setUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ID != setUUID || res.Title != "Test Set" || res.CardsTotal != 10 || res.CardsDue != 5 || res.Author.FirstName != "Alice" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Not found
	_, err = svc.GetSet(context.Background(), 100, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSetService_JoinSetByShareCode(t *testing.T) {
	setUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(setUUID.String())

	mock := &mockQuerier{
		getSetByShareCodeFunc: func(_ context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
			if arg.ShareCode == "101101" {
				return db.GetSetByShareCodeRow{
					ID:              pgUUID,
					Title:           "Course Set",
					AuthorID:        200,
					AuthorFirstName: "Bob",
					ShareCode:       "101101",
					CardsTotal:      20,
					CardsDue:        20,
				}, nil
			}
			return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
		},
		joinSetFunc: func(_ context.Context, arg db.JoinSetParams) (db.UserSet, error) {
			return db.UserSet{UserID: arg.UserID, SetID: arg.SetID}, nil
		},
		initUserFactProgressFunc: func(_ context.Context, _ db.InitUserFactProgressParams) error {
			return nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	res, err := svc.JoinSetByShareCode(context.Background(), 100, "101101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ID != setUUID || res.Title != "Course Set" {
		t.Fatalf("unexpected set: %+v", res)
	}

	// Not found share code
	_, err = svc.JoinSetByShareCode(context.Background(), 100, "999999")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestSetService_DeleteSet(t *testing.T) {
	setUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(setUUID.String())

	deleted := false
	left := false

	mock := &mockQuerier{
		isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
			return arg.AuthorID == 100, nil
		},
		deleteSetFunc: func(_ context.Context, _ db.DeleteSetParams) (int64, error) {
			deleted = true
			return 1, nil
		},
		leaveSetFunc: func(_ context.Context, _ db.LeaveSetParams) (int64, error) {
			left = true
			return 1, nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	// Author deletes
	if err := svc.DeleteSet(context.Background(), 100, setUUID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleteSetFunc to be called")
	}

	// Non-author leaves
	if err := svc.DeleteSet(context.Background(), 200, setUUID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !left {
		t.Fatal("expected leaveSetFunc to be called")
	}
}

func TestCardService_AnswerQuestion(t *testing.T) {
	cardUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(cardUUID.String())

	recorded := false
	progressUpdated := false

	mock := &mockQuerier{
		getCardByIDFunc: func(_ context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error) {
			if arg.UserID != 100 {
				return db.GetCardByIDRow{}, pgx.ErrNoRows
			}
			return db.GetCardByIDRow{
				ID:          pgUUID,
				FactID:      "fact-1",
				Kind:        "choice",
				Question:    "What is C++?",
				AnswerText:  pgtype.Text{String: "A programming language", Valid: true},
				Explanation: "Because it is.",
				SourceQuote: "C++ is a language.",
			}, nil
		},
		recordAnswerResultFunc: func(_ context.Context, arg db.RecordAnswerResultParams) (db.AnswerResult, error) {
			recorded = true
			if !arg.Correct {
				t.Fatalf("expected correct answer, got incorrect")
			}
			return db.AnswerResult{}, nil
		},
		updateFactProgressOnAnswerFunc: func(_ context.Context, _ db.UpdateFactProgressOnAnswerParams) (db.UserFactProgress, error) {
			progressUpdated = true
			return db.UserFactProgress{}, nil
		},
	}

	svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

	res, err := svc.AnswerQuestion(context.Background(), 100, cardUUID, "A programming language")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsCorrect || res.UserAnswer != "A programming language" {
		t.Fatalf("unexpected answer response: %+v", res)
	}
	if !recorded || !progressUpdated {
		t.Fatal("expected answer recording and progress update")
	}
}

func TestHomescreenService_GetToday(t *testing.T) {
	mock := &mockQuerier{
		getUserByIDFunc: func(_ context.Context, id int64) (db.User, error) {
			return db.User{ID: id, FirstName: "Tester"}, nil
		},
		countUserActiveDaysFunc: func(_ context.Context, _ int64) (int32, error) {
			return 4, nil
		},
		countTotalDueCardsForUserFunc: func(_ context.Context, _ int64) (int32, error) {
			return 12, nil
		},
		getUserSetsFunc: func(_ context.Context, _ int64) ([]db.GetUserSetsRow, error) {
			return []db.GetUserSetsRow{
				{
					Title:           "Set 1",
					AuthorID:        300,
					AuthorFirstName: "Alice",
					CardsTotal:      10,
					CardsDue:        5,
				},
			}, nil
		},
	}

	svc := usecase.NewHomescreenService(mock, usecase.NewUserService(mock))

	today, err := svc.GetToday(context.Background(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if today.User.ID != 100 || today.User.FirstName != "Tester" || today.ActiveDays != 4 || today.DueCount != 12 {
		t.Fatalf("unexpected today data: %+v", today)
	}
	if today.EstimatedMinutes <= 0 {
		t.Fatalf("expected estimatedMinutes > 0, got %d", today.EstimatedMinutes)
	}
}

func TestHomescreenService_SendResults(t *testing.T) {
	cardUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(cardUUID.String())

	count := 0

	mock := &mockQuerier{
		getCardByIDFunc: func(_ context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error) {
			if arg.UserID != 100 {
				return db.GetCardByIDRow{}, pgx.ErrNoRows
			}
			return db.GetCardByIDRow{
				ID:     pgUUID,
				FactID: "fact-1",
			}, nil
		},
		recordAnswerResultFunc: func(_ context.Context, _ db.RecordAnswerResultParams) (db.AnswerResult, error) {
			count++
			return db.AnswerResult{}, nil
		},
		updateFactProgressOnAnswerFunc: func(_ context.Context, _ db.UpdateFactProgressOnAnswerParams) (db.UserFactProgress, error) {
			return db.UserFactProgress{}, nil
		},
	}

	svc := usecase.NewHomescreenService(mock, usecase.NewUserService(mock))

	results := []contracts.AnswerResult{
		{
			CardId:     cardUUID,
			Correct:    true,
			AnsweredAt: time.Now(),
		},
	}

	if err := svc.SendResults(context.Background(), 100, results); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 result recorded, got %d", count)
	}
}

func TestCardService_UpdateCard_Authorization(t *testing.T) {
	cardUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(cardUUID.String())

	setUUID := uuid.New()
	var pgSetUUID pgtype.UUID
	_ = pgSetUUID.Scan(setUUID.String())

	authorID := int64(100)
	otherUserID := int64(999)

	mock := &mockQuerier{
		getCardByIDForAuthorFunc: func(_ context.Context, arg db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
			if arg.AuthorID == authorID && arg.ID == pgUUID {
				return db.GetCardByIDForAuthorRow{
					ID:         pgUUID,
					SetID:      pgSetUUID,
					Kind:       "flip",
					Question:   "Old question",
					AnswerText: pgtype.Text{String: "Answer", Valid: true},
				}, nil
			}
			return db.GetCardByIDForAuthorRow{}, pgx.ErrNoRows
		},
		updateCardFunc: func(_ context.Context, arg db.UpdateCardParams) (db.Card, error) {
			if arg.AuthorID != authorID {
				return db.Card{}, pgx.ErrNoRows
			}
			return db.Card{
				ID:         arg.ID,
				Kind:       "flip",
				Question:   arg.Question,
				AnswerText: arg.AnswerText,
			}, nil
		},
	}

	svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

	// Author succeeds
	req := &contracts.UpdateCardRequest{
		Question: contracts.NewOptString("New question"),
	}
	res, err := svc.UpdateCard(context.Background(), authorID, cardUUID, req)
	if err != nil {
		t.Fatalf("unexpected error for author: %v", err)
	}
	if res.Question != "New question" {
		t.Fatalf("expected 'New question', got %s", res.Question)
	}

	// Non-author fails with ErrNotFound
	_, err = svc.UpdateCard(context.Background(), otherUserID, cardUUID, req)
	if !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-author, got %v", err)
	}
}

func TestCardService_DeleteCard_Authorization(t *testing.T) {
	cardUUID := uuid.New()
	authorID := int64(100)
	otherUserID := int64(999)

	mock := &mockQuerier{
		deleteCardFunc: func(_ context.Context, arg db.DeleteCardParams) (int64, error) {
			if arg.AuthorID == authorID {
				return 1, nil
			}
			return 0, nil
		},
	}

	svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

	// Author succeeds
	if err := svc.DeleteCard(context.Background(), authorID, cardUUID); err != nil {
		t.Fatalf("unexpected error for author: %v", err)
	}

	// Non-author fails with ErrNotFound
	err := svc.DeleteCard(context.Background(), otherUserID, cardUUID)
	if !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-author, got %v", err)
	}
}

func TestSetService_GetCardsBySetID_Authorization(t *testing.T) {
	setUUID := uuid.New()
	var pgSetUUID pgtype.UUID
	_ = pgSetUUID.Scan(setUUID.String())

	memberID := int64(100)
	nonMemberID := int64(999)

	mock := &mockQuerier{
		getSetByIDFunc: func(_ context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error) {
			if arg.UserID == memberID && arg.ID == pgSetUUID {
				return db.GetSetByIDRow{ID: pgSetUUID, Title: "Set 1"}, nil
			}
			return db.GetSetByIDRow{}, pgx.ErrNoRows
		},
		getCardsBySetIDFunc: func(_ context.Context, arg db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error) {
			if arg.UserID == memberID && arg.SetID == pgSetUUID {
				return []db.GetCardsBySetIDRow{
					{
						ID:       pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
						SetID:    pgSetUUID,
						Kind:     "flip",
						Question: "Q1",
					},
				}, nil
			}
			return nil, nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	// Member succeeds
	cards, err := svc.GetCardsBySetID(context.Background(), memberID, setUUID)
	if err != nil {
		t.Fatalf("unexpected error for member: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}

	// Non-member fails with ErrNotFound
	_, err = svc.GetCardsBySetID(context.Background(), nonMemberID, setUUID)
	if !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-member, got %v", err)
	}
}

func TestSetService_DeleteSet_NonMember(t *testing.T) {
	setUUID := uuid.New()
	nonMemberID := int64(999)

	mock := &mockQuerier{
		isSetAuthorFunc: func(_ context.Context, _ db.IsSetAuthorParams) (bool, error) {
			return false, nil
		},
		leaveSetFunc: func(_ context.Context, _ db.LeaveSetParams) (int64, error) {
			return 0, nil // User was not enrolled
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	err := svc.DeleteSet(context.Background(), nonMemberID, setUUID)
	if !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when leaving unenrolled set, got %v", err)
	}
}
