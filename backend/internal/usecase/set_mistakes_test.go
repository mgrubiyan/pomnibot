package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestSetService_GetSetMistakes(t *testing.T) {
	setID := uuid.New()
	pgSet := pgtype.UUID{Bytes: setID, Valid: true}
	author := pgtype.UUID{Bytes: testUser1, Valid: true}
	bob := pgtype.UUID{Bytes: testUser2, Valid: true}
	carol := pgtype.UUID{Bytes: testUser3, Valid: true}
	dan := pgtype.UUID{Bytes: testOther, Valid: true}
	keys := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-000000000f01"), Valid: true}
	indexes := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-000000000f02"), Valid: true}
	forms := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-000000000f03"), Valid: true}
	joins := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-000000000f04"), Valid: true}
	transactions := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-000000000f05"), Valid: true}

	mock := &mockQuerier{
		getSetByIDFunc: func(_ context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error) {
			if arg.ID != pgSet {
				return db.GetSetByIDRow{}, pgx.ErrNoRows
			}
			return db.GetSetByIDRow{ID: pgSet, Title: "БД", AuthorID: author, AuthorFirstName: "Alice"}, nil
		},
		isSetAuthorFunc: func(_ context.Context, arg db.IsSetAuthorParams) (bool, error) {
			return arg.ID == pgSet && arg.AuthorID == author, nil
		},
		getFactsBySetIDFunc: func(_ context.Context, _ pgtype.UUID) ([]db.GetFactsBySetIDRow, error) {
			return []db.GetFactsBySetIDRow{
				{ID: forms, SetID: pgSet, Name: "Нормальные формы"},
				{ID: joins, SetID: pgSet, Name: "Соединения"},
				{ID: transactions, SetID: pgSet, Name: "Транзакции"},
				{ID: indexes, SetID: pgSet, Name: "Индексы"},
				{ID: keys, SetID: pgSet, Name: "Ключи"},
			}, nil
		},
		getSetStudentsFunc: func(_ context.Context, _ pgtype.UUID) ([]db.GetSetStudentsRow, error) {
			return []db.GetSetStudentsRow{
				{ID: bob, FirstName: "Bob"},
				{ID: carol, FirstName: "Carol"},
				{ID: dan, FirstName: "Dan"},
			}, nil
		},
		getSetFactAnswersFunc: func(_ context.Context, _ pgtype.UUID) ([]db.GetSetFactAnswersRow, error) {
			return []db.GetSetFactAnswersRow{
				{FactID: keys, UserID: bob, Answers: 4, Mistakes: 1},
				{FactID: keys, UserID: carol, Answers: 3, Mistakes: 1},
				{FactID: indexes, UserID: carol, Answers: 2, Mistakes: 0},
				{FactID: indexes, UserID: bob, Answers: 5, Mistakes: 4},
				{FactID: transactions, UserID: bob, Answers: 3, Mistakes: 2},
				{FactID: joins, UserID: bob, Answers: 2, Mistakes: 0},
			}, nil
		},
	}
	svc := usecase.NewSetService(mock, usecase.NewUserService(mock))

	t.Run("author sees facts with the most mistaken members first", func(t *testing.T) {
		res, err := svc.GetSetMistakes(context.Background(), testUser1, setID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.SetId != setID {
			t.Fatalf("set id = %s, want %s", res.SetId, setID)
		}

		// Ключи: both members erred, if only once each; Индексы and
		// Транзакции: one member each, more mistakes first; Соединения:
		// answered without mistakes; Нормальные формы: nobody answered.
		wantFacts := []struct {
			name                        string
			answers, mistakes           int
			answered, mistaken, entries int
		}{
			{"Ключи", 7, 2, 2, 2, 2},
			{"Индексы", 7, 4, 2, 1, 2},
			{"Транзакции", 3, 2, 1, 1, 1},
			{"Соединения", 2, 0, 1, 0, 1},
			{"Нормальные формы", 0, 0, 0, 0, 0},
		}
		if len(res.Facts) != len(wantFacts) {
			t.Fatalf("got %d facts, want %d: %+v", len(res.Facts), len(wantFacts), res.Facts)
		}
		for i, want := range wantFacts {
			got := res.Facts[i]
			if got.FactName != want.name || got.Answers != want.answers || got.Mistakes != want.mistakes ||
				got.StudentsAnswered != want.answered || got.StudentsMistaken != want.mistaken || len(got.Students) != want.entries {
				t.Errorf("fact %d = %+v, want %+v", i, got, want)
			}
		}

		// Members inside a fact keep the order they joined in.
		indexStudents := res.Facts[1].Students
		if indexStudents[0].UserId != testUser2 || indexStudents[0].Answers != 5 || indexStudents[0].Mistakes != 4 ||
			indexStudents[1].UserId != testUser3 || indexStudents[1].Answers != 2 || indexStudents[1].Mistakes != 0 {
			t.Errorf("index students = %+v", indexStudents)
		}

		wantStudents := []struct {
			id                uuid.UUID
			answers, mistakes int
		}{
			{testUser2, 14, 7},
			{testUser3, 5, 1},
			{testOther, 0, 0},
		}
		if len(res.Students) != len(wantStudents) {
			t.Fatalf("got %d students, want %d", len(res.Students), len(wantStudents))
		}
		for i, want := range wantStudents {
			got := res.Students[i]
			if got.User.ID != want.id || got.Answers != want.answers || got.Mistakes != want.mistakes {
				t.Errorf("student %d = %+v, want %+v", i, got, want)
			}
		}
		if res.Students[2].User.FirstName != "Dan" {
			t.Errorf("student without answers lost the name: %+v", res.Students[2].User)
		}
	})

	t.Run("member is forbidden", func(t *testing.T) {
		if _, err := svc.GetSetMistakes(context.Background(), testUser2, setID); !errors.Is(err, usecase.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unknown set is not found", func(t *testing.T) {
		if _, err := svc.GetSetMistakes(context.Background(), testUser1, uuid.New()); !errors.Is(err, usecase.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
