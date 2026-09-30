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
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
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
	if !res.UserRank.IsSet() || res.UserRank.Value != 1 {
		t.Errorf("expected UserRank 1, got %+v", res.UserRank)
	}
	if !res.UserPercentile.IsSet() || res.UserPercentile.Value != 0 {
		t.Errorf("expected UserPercentile 0, got %+v", res.UserPercentile)
	}

	// Not found
	_, err = svc.GetSet(context.Background(), 100, uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSetService_GetSetLeaderboard(t *testing.T) {
	setUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(setUUID.String())

	authorID := int64(100)
	memberID := int64(200)

	mock := &mockQuerier{
		getSetByIDFunc: func(_ context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error) {
			if arg.ID == pgUUID {
				return db.GetSetByIDRow{
					ID:              pgUUID,
					Title:           "Math Set",
					AuthorID:        authorID,
					AuthorFirstName: "Alice",
				}, nil
			}
			return db.GetSetByIDRow{}, pgx.ErrNoRows
		},
		isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
			if arg.ID == pgUUID && arg.AuthorID == authorID {
				return true, nil
			}
			return false, nil
		},
		getSetLeaderboardFunc: func(_ context.Context, setID pgtype.UUID) ([]db.GetSetLeaderboardRow, error) {
			if setID == pgUUID {
				return []db.GetSetLeaderboardRow{
					{
						UserID:     authorID,
						FirstName:  "Alice",
						Percentile: 100,
						Rank:       1,
					},
					{
						UserID:     memberID,
						FirstName:  "Bob",
						Percentile: 50,
						Rank:       2,
					},
				}, nil
			}
			return nil, nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	t.Run("Author gets leaderboard successfully", func(t *testing.T) {
		res, err := svc.GetSetLeaderboard(context.Background(), authorID, setUUID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.SetId != setUUID {
			t.Errorf("expected set ID %s, got %s", setUUID, res.SetId)
		}
		if len(res.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(res.Items))
		}
		if res.Items[0].User.ID != authorID || res.Items[0].Rank != 1 || res.Items[0].Percentile != 100 {
			t.Errorf("unexpected item 0: %+v", res.Items[0])
		}
		if res.Items[1].User.ID != memberID || res.Items[1].Rank != 2 || res.Items[1].Percentile != 50 {
			t.Errorf("unexpected item 1: %+v", res.Items[1])
		}
	})

	t.Run("Non-author is forbidden", func(t *testing.T) {
		_, err := svc.GetSetLeaderboard(context.Background(), memberID, setUUID)
		if !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("Non-existent set returns ErrNotFound", func(t *testing.T) {
		_, err := svc.GetSetLeaderboard(context.Background(), authorID, uuid.New())
		if !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
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

type fakeModel struct{ calls int }

func (m *fakeModel) Complete(context.Context, providers.Request) (providers.Response, error) {
	m.calls++
	return providers.Response{Content: []byte(`{"correct": true, "reason": "Упорядоченный — то же, что отсортированный."}`)}, nil
}

// A typed answer is checked by meaning and nothing is recorded: the feed
// sends results itself.
func TestCardService_CheckAnswer(t *testing.T) {
	cardUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(cardUUID.String())

	mock := &mockQuerier{
		getCardByIDFunc: func(_ context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error) {
			if arg.UserID != 100 {
				return db.GetCardByIDRow{}, pgx.ErrNoRows
			}
			return db.GetCardByIDRow{
				ID:          pgUUID,
				FactID:      "fact-1",
				Kind:        "input",
				Question:    "На каком массиве применим бинарный поиск?",
				AnswerText:  pgtype.Text{String: "на отсортированном массиве", Valid: true},
				SourceQuote: "Бинарный поиск работает только на отсортированном массиве",
			}, nil
		},
		recordAnswerResultFunc: func(context.Context, db.RecordAnswerResultParams) (db.AnswerResult, error) {
			t.Fatal("a check must not record the answer")
			return db.AnswerResult{}, nil
		},
	}
	model := &fakeModel{}
	svc := usecase.NewCardService(mock, usecase.NewUserService(mock), usecase.WithGrader(&grader.Grader{Model: model}))

	res, err := svc.CheckAnswer(context.Background(), 100, cardUUID, "массив должен быть упорядочен")
	if err != nil || !res.IsCorrect || res.Method != contracts.CheckAnswerResponseMethodModel || res.Reason.Value == "" {
		t.Fatalf("CheckAnswer() = %+v, %v; want correct by the model with a reason", res, err)
	}

	res, err = svc.CheckAnswer(context.Background(), 100, cardUUID, "Отсортированный массив.")
	if err != nil || !res.IsCorrect || res.Method != contracts.CheckAnswerResponseMethodLocal || model.calls != 1 {
		t.Fatalf("CheckAnswer() = %+v, %v, model calls %d; want correct locally", res, err, model.calls)
	}

	if _, err := svc.CheckAnswer(context.Background(), 200, cardUUID, "что угодно"); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("someone else's card: err = %v, want ErrNotFound", err)
	}
}

func TestHomescreenService_GetToday(t *testing.T) {
	futureTime := time.Now().Add(24 * time.Hour)
	mock := &mockQuerier{
		getUserByIDFunc: func(_ context.Context, id int64) (db.User, error) {
			return db.User{ID: id, FirstName: "Tester"}, nil
		},
		getNextReviewDateForUserFunc: func(_ context.Context, _ int64) (pgtype.Timestamptz, error) {
			return pgtype.Timestamptz{Time: futureTime, Valid: true}, nil
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
	if today.User.ID != 100 || today.User.FirstName != "Tester" || today.DueCount != 12 {
		t.Fatalf("unexpected today data: %+v", today)
	}
	if !today.NextReviewAt.Set || today.NextReviewAt.Value.Unix() != futureTime.Unix() {
		t.Fatalf("expected NextReviewAt to be set to futureTime, got %+v", today.NextReviewAt)
	}
	if today.EstimatedMinutes <= 0 {
		t.Fatalf("expected estimatedMinutes > 0, got %d", today.EstimatedMinutes)
	}
}

func TestHomescreenService_GetToday_NoNextReview(t *testing.T) {
	mock := &mockQuerier{
		getUserByIDFunc: func(_ context.Context, id int64) (db.User, error) {
			return db.User{ID: id, FirstName: "Tester"}, nil
		},
		getNextReviewDateForUserFunc: func(_ context.Context, _ int64) (pgtype.Timestamptz, error) {
			return pgtype.Timestamptz{Valid: false}, nil
		},
		countTotalDueCardsForUserFunc: func(_ context.Context, _ int64) (int32, error) {
			return 0, nil
		},
		getUserSetsFunc: func(_ context.Context, _ int64) ([]db.GetUserSetsRow, error) {
			return nil, nil
		},
	}

	svc := usecase.NewHomescreenService(mock, usecase.NewUserService(mock))

	today, err := svc.GetToday(context.Background(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if today.NextReviewAt.Set && !today.NextReviewAt.Null {
		t.Fatalf("expected NextReviewAt to be unset or null, got %+v", today.NextReviewAt)
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

func TestCardService_UpdateCard_FieldsAndKinds(t *testing.T) {
	cardUUID := uuid.New()
	var pgUUID pgtype.UUID
	_ = pgUUID.Scan(cardUUID.String())

	setUUID := uuid.New()
	var pgSetUUID pgtype.UUID
	_ = pgSetUUID.Scan(setUUID.String())

	authorID := int64(100)

	t.Run("Update boolean card with answer and topic", func(t *testing.T) {
		var updatedArg db.UpdateCardParams
		mock := &mockQuerier{
			getCardByIDForAuthorFunc: func(_ context.Context, _ db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
				return db.GetCardByIDForAuthorRow{
					ID:         pgUUID,
					SetID:      pgSetUUID,
					Kind:       "boolean",
					Question:   "Initial question",
					AnswerText: pgtype.Text{String: "false", Valid: true},
					Topic:      "Initial Topic",
				}, nil
			},
			updateCardFunc: func(_ context.Context, arg db.UpdateCardParams) (db.Card, error) {
				updatedArg = arg
				return db.Card{
					ID:          arg.ID,
					Kind:        "boolean",
					Question:    arg.Question,
					AnswerText:  arg.AnswerText,
					Explanation: arg.Explanation,
					SourceQuote: arg.SourceQuote,
					SourceRef:   arg.SourceRef,
				}, nil
			},
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

		req := &contracts.UpdateCardRequest{
			Question:    contracts.NewOptString("Is preprocessor first?"),
			Explanation: contracts.NewOptString("Yes, it is the first stage."),
			Topic:       contracts.NewOptString("Compilation"),
			Answer: contracts.NewOptCardAnswer(contracts.CardAnswer{
				Type: contracts.BoolCardAnswer,
				Bool: true,
			}),
		}

		res, err := svc.UpdateCard(context.Background(), authorID, cardUUID, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Question != "Is preprocessor first?" {
			t.Errorf("expected updated question, got %s", res.Question)
		}
		if res.Topic != "Compilation" {
			t.Errorf("expected topic Compilation, got %s", res.Topic)
		}
		if res.Explanation != "Yes, it is the first stage." {
			t.Errorf("expected updated explanation, got %s", res.Explanation)
		}
		if updatedArg.AnswerText.String != "true" {
			t.Errorf("expected db AnswerText 'true', got %s", updatedArg.AnswerText.String)
		}
		if !res.Answer.Bool {
			t.Errorf("expected Bool answer true")
		}
	})

	t.Run("Update table card layout", func(t *testing.T) {
		colsDeleted := false
		itemsDeleted := false
		createdCols := 0
		createdItems := 0

		mock := &mockQuerier{
			getCardByIDForAuthorFunc: func(_ context.Context, _ db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
				return db.GetCardByIDForAuthorRow{
					ID:       pgUUID,
					SetID:    pgSetUUID,
					Kind:     "table",
					Question: "Categorize items",
				}, nil
			},
			updateCardFunc: func(_ context.Context, arg db.UpdateCardParams) (db.Card, error) {
				return db.Card{
					ID:       arg.ID,
					Kind:     "table",
					Question: arg.Question,
				}, nil
			},
			deleteCardTableColumnsFunc: func(_ context.Context, _ pgtype.UUID) error {
				colsDeleted = true
				return nil
			},
			deleteCardTableItemsFunc: func(_ context.Context, _ pgtype.UUID) error {
				itemsDeleted = true
				return nil
			},
			createCardTableColumnFunc: func(_ context.Context, _ db.CreateCardTableColumnParams) (db.CardTableColumn, error) {
				createdCols++
				return db.CardTableColumn{}, nil
			},
			createCardTableItemFunc: func(_ context.Context, _ db.CreateCardTableItemParams) (db.CardTableItem, error) {
				createdItems++
				return db.CardTableItem{}, nil
			},
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

		tableReq := contracts.TableLayout{
			Columns: []string{"Col A", "Col B"},
			Items: []contracts.TableItem{
				{Text: "Item 1", Column: "Col A"},
				{Text: "Item 2", Column: "Col B"},
			},
		}

		req := &contracts.UpdateCardRequest{
			Table: contracts.NewOptTableLayout(tableReq),
		}

		res, err := svc.UpdateCard(context.Background(), authorID, cardUUID, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !colsDeleted || !itemsDeleted {
			t.Errorf("expected old table columns and items to be deleted")
		}
		if createdCols != 2 || createdItems != 2 {
			t.Errorf("expected 2 columns and 2 items created, got %d cols and %d items", createdCols, createdItems)
		}
		if !res.Table.IsSet() || len(res.Table.Value.Columns) != 2 {
			t.Errorf("expected table layout in response")
		}
	})

	t.Run("Update card kind to choice with options", func(t *testing.T) {
		var updatedKind string
		mock := &mockQuerier{
			getCardByIDForAuthorFunc: func(_ context.Context, _ db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
				return db.GetCardByIDForAuthorRow{
					ID:         pgUUID,
					SetID:      pgSetUUID,
					Kind:       "input",
					Question:   "Initial question",
					AnswerText: pgtype.Text{String: "Option 1", Valid: true},
				}, nil
			},
			updateCardFunc: func(_ context.Context, arg db.UpdateCardParams) (db.Card, error) {
				updatedKind = arg.Kind
				return db.Card{
					ID:         arg.ID,
					Kind:       arg.Kind,
					Question:   arg.Question,
					AnswerText: arg.AnswerText,
				}, nil
			},
			deleteCardOptionsFunc: func(_ context.Context, _ pgtype.UUID) error {
				return nil
			},
			createCardOptionFunc: func(_ context.Context, _ db.CreateCardOptionParams) (db.CardOption, error) {
				return db.CardOption{}, nil
			},
		}

		svc := usecase.NewCardService(mock, usecase.NewUserService(mock))

		req := &contracts.UpdateCardRequest{
			Kind:    contracts.NewOptCardKind(contracts.CardKindChoice),
			Options: []string{"Option 1", "Option 2"},
			Answer: contracts.NewOptCardAnswer(contracts.CardAnswer{
				Type:   contracts.StringCardAnswer,
				String: "Option 1",
			}),
		}

		res, err := svc.UpdateCard(context.Background(), authorID, cardUUID, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updatedKind != "choice" {
			t.Errorf("expected db kind 'choice', got %s", updatedKind)
		}
		if res.Kind != contracts.CardKindChoice {
			t.Errorf("expected res.Kind CardKindChoice, got %v", res.Kind)
		}
		if len(res.Options) != 2 {
			t.Errorf("expected 2 options in res")
		}
	})
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
	cardsByID, err := svc.GetCardsBySetID(context.Background(), memberID, setUUID)
	if err != nil {
		t.Fatalf("unexpected error for member: %v", err)
	}
	if len(cardsByID) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cardsByID))
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

func TestSetService_SaveGeneratedSet(t *testing.T) {
	ctx := context.Background()
	const userID = int64(123)
	const title = "Конспект по биологии"

	mock := &mockQuerier{
		ensureUserFunc: func(_ context.Context, id int64) (db.User, error) {
			return db.User{ID: id}, nil
		},
		getSetByShareCodeFunc: func(_ context.Context, _ db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
			return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
		},
		createSetFunc: func(_ context.Context, arg db.CreateSetParams) (db.Set, error) {
			var setUUID pgtype.UUID
			_ = setUUID.Scan("22222222-2222-2222-2222-222222222222")
			return db.Set{
				ID:        setUUID,
				Title:     arg.Title,
				AuthorID:  arg.AuthorID,
				ShareCode: arg.ShareCode,
			}, nil
		},
		joinSetFunc: func(_ context.Context, arg db.JoinSetParams) (db.UserSet, error) {
			return db.UserSet{UserID: arg.UserID, SetID: arg.SetID}, nil
		},
		upsertTopicFunc: func(_ context.Context, name string) (db.Topic, error) {
			return db.Topic{Name: name}, nil
		},
		createFactFunc: func(_ context.Context, arg db.CreateFactParams) (db.Fact, error) {
			return db.Fact{ID: arg.ID, SetID: arg.SetID, Name: arg.Name}, nil
		},
		createCardFunc: func(_ context.Context, arg db.CreateCardParams) (db.Card, error) {
			return db.Card{ID: arg.ID, FactID: arg.FactID, Kind: arg.Kind}, nil
		},
		initUserFactProgressFunc: func(_ context.Context, _ db.InitUserFactProgressParams) error {
			return nil
		},
	}

	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))
	genResult := generator.Result{
		Facts: []cards.Fact{
			{ID: "f1", Name: "Fact 1", Topic: "Bio"},
		},
		Cards: []cards.Card{
			{FactID: "f1", Kind: cards.KindFlip, Question: "Q1", Answer: "A1"},
		},
	}

	t.Run("success saves generated set", func(t *testing.T) {
		res, err := svc.SaveGeneratedSet(ctx, userID, title, genResult)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Title != title {
			t.Errorf("expected title %q, got %q", title, res.Title)
		}
		if res.CardsTotal != 1 {
			t.Errorf("expected 1 card, got %d", res.CardsTotal)
		}
	})
}
