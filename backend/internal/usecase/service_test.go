package usecase_test

import (
	"context"
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
					ID:         pgUUID,
					Title:      "Test Set",
					AuthorID:   100,
					AuthorName: "Alice",
					ShareCode:  "123456",
					CardsTotal: 10,
					CardsDue:   5,
				}, nil
			}
			return db.GetSetByIDRow{}, pgx.ErrNoRows
		},
	}

	svc := usecase.NewSetService(mock)

	// Success
	res, err := svc.GetSet(context.Background(), 100, setUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ID != setUUID || res.Title != "Test Set" || res.CardsTotal != 10 || res.CardsDue != 5 {
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
					ID:         pgUUID,
					Title:      "Course Set",
					AuthorID:   200,
					AuthorName: "Bob",
					ShareCode:  "101101",
					CardsTotal: 20,
					CardsDue:   20,
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

	svc := usecase.NewSetService(mock)

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
		deleteSetFunc: func(_ context.Context, _ db.DeleteSetParams) error {
			deleted = true
			return nil
		},
		leaveSetFunc: func(_ context.Context, _ db.LeaveSetParams) error {
			left = true
			return nil
		},
	}

	svc := usecase.NewSetService(mock)

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
		getCardByIDFunc: func(_ context.Context, _ pgtype.UUID) (db.GetCardByIDRow, error) {
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

	svc := usecase.NewCardService(mock)

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
			return db.User{ID: id, Name: "Tester"}, nil
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
					Title:      "Set 1",
					AuthorName: "Alice",
					CardsTotal: 10,
					CardsDue:   5,
				},
			}, nil
		},
	}

	svc := usecase.NewHomescreenService(mock)

	today, err := svc.GetToday(context.Background(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if today.UserName != "Tester" || today.ActiveDays != 4 || today.DueCount != 12 {
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
		getCardByIDFunc: func(_ context.Context, _ pgtype.UUID) (db.GetCardByIDRow, error) {
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

	svc := usecase.NewHomescreenService(mock)

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
