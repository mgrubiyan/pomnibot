package usecase_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestChoiceCheckRequiresAllAnswers(t *testing.T) {
	q := &mockQuerier{getCardByIDFunc: func(context.Context, db.GetCardByIDParams) (db.GetCardByIDRow, error) {
		return db.GetCardByIDRow{Kind: "choice", AnswerText: pgtype.Text{String: `["A","B"]`, Valid: true}}, nil
	}}
	svc := usecase.NewCardService(q, usecase.NewUserService(q))
	for _, tc := range []struct {
		given string
		want  bool
	}{{`["B","A"]`, true}, {`["A"]`, false}, {`["A","B","C"]`, false}, {`["A","A"]`, false}} {
		r, err := svc.CheckAnswer(context.Background(), 1, uuid.New(), tc.given)
		if err != nil || r.IsCorrect != tc.want || r.Method != contracts.CheckAnswerResponseMethodLocal {
			t.Fatalf("%s: %+v %v", tc.given, r, err)
		}
	}
}

func TestChoiceAnswerOnlyEditUpdatesAllOptionFlags(t *testing.T) {
	var saved []db.CreateCardOptionParams
	q := &mockQuerier{
		getCardByIDForAuthorFunc: func(context.Context, db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
			return db.GetCardByIDForAuthorRow{Kind: "choice", Question: "Q", AnswerText: pgtype.Text{String: "A", Valid: true}}, nil
		},
		getCardOptionsFunc: func(context.Context, pgtype.UUID) ([]db.CardOption, error) {
			return []db.CardOption{{Text: "A", IsCorrect: true}, {Text: "B"}, {Text: "C"}}, nil
		},
		updateCardFunc: func(_ context.Context, p db.UpdateCardParams) (db.Card, error) {
			return db.Card{Kind: p.Kind, Question: p.Question, AnswerText: p.AnswerText}, nil
		},
		createCardOptionFunc: func(_ context.Context, p db.CreateCardOptionParams) (db.CardOption, error) {
			saved = append(saved, p)
			return db.CardOption{}, nil
		},
	}
	svc := usecase.NewCardService(q, usecase.NewUserService(q))
	r, err := svc.UpdateCard(context.Background(), 1, uuid.New(), &contracts.UpdateCardRequest{Answer: contracts.NewOptCardAnswer(contracts.NewStringArrayCardAnswer([]string{"A", "B"}))})
	if err != nil || len(saved) != 3 || !saved[0].IsCorrect || !saved[1].IsCorrect || saved[2].IsCorrect || len(r.Answer.StringArray) != 2 {
		t.Fatalf("%+v %+v %v", r, saved, err)
	}
}
