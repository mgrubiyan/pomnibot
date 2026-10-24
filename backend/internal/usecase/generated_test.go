package usecase_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type reportQuerier struct {
	*mockQuerier
	reports []db.SaveProcessingReportParams
}

func (q *reportQuerier) SaveProcessingReport(_ context.Context, p db.SaveProcessingReportParams) (int64, error) {
	q.reports = append(q.reports, p)
	return 1, nil
}

func TestSaveGeneratedFactsAssignsUUIDsAndConvertsCards(t *testing.T) {
	var facts []db.CreateFactParams
	var saved []db.CreateCardParams
	var options []db.CreateCardOptionParams
	var columns []db.CreateCardTableColumnParams
	var items []db.CreateCardTableItemParams
	q := &reportQuerier{mockQuerier: &mockQuerier{
		getSetByShareCodeFunc: func(context.Context, string) (db.GetSetByShareCodeRow, error) {
			return db.GetSetByShareCodeRow{}, pgx.ErrNoRows
		},
		createSetFunc: func(_ context.Context, p db.CreateSetParams) (db.Set, error) {
			return db.Set{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Title: p.Title}, nil
		},
		createFactFunc: func(_ context.Context, p db.CreateFactParams) (db.Fact, error) {
			facts = append(facts, p)
			return db.Fact{ID: p.ID}, nil
		},
		createCardFunc: func(_ context.Context, p db.CreateCardParams) (db.Card, error) {
			saved = append(saved, p)
			return db.Card{ID: p.ID}, nil
		},
		createCardOptionFunc: func(_ context.Context, p db.CreateCardOptionParams) (db.CardOption, error) {
			options = append(options, p)
			return db.CardOption{}, nil
		},
		createCardTableColumnFunc: func(_ context.Context, p db.CreateCardTableColumnParams) (db.CardTableColumn, error) {
			columns = append(columns, p)
			return db.CardTableColumn{}, nil
		},
		createCardTableItemFunc: func(_ context.Context, p db.CreateCardTableItemParams) (db.CardTableItem, error) {
			items = append(items, p)
			return db.CardTableItem{}, nil
		},
	}}
	base := func(q, s, quote string) generator.CardBase {
		return generator.CardBase{Question: q, Source: s, SourceQuote: quote, QuestionSvg: `<svg viewBox="0 0 1 1"/>`, ExplanationSvg: `<svg viewBox="0 0 2 2"/>`}
	}
	r := generator.Result{Facts: []generator.Fact{
		{Name: "First", Pages: []int{2, 3}, Cards: []generator.Card{
			generator.FlipCard{CardBase: base("Flip", "Flip source", "Quote 1"), Answer: "A"},
			generator.BooleanCard{CardBase: base("Boolean", "Boolean source", ""), Answer: false},
			generator.ChoiceCard{CardBase: base("Choice", "Choice source", "Quote 3"), RightAnswers: []string{"A", "B"}, WrongAnswers: []string{"C"}},
			generator.TableCard{CardBase: base("Table", "Table source", ""), Columns: []generator.Column{{Name: "X  ", Rows: []string{"    $\\frac{1}{2}$  \n"}}, {Name: "Y", Rows: []string{"row Y"}}}},
			generator.InputCard{CardBase: base("Input differs", "Input source", "Quote 5"), Answer: "Different answer"},
		}},
		{Name: "Second", Pages: []int{1}, Cards: []generator.Card{generator.FlipCard{CardBase: base("Other", "Other source", ""), Answer: "B"}, generator.InputCard{CardBase: base("Other input", "Other source", ""), Answer: "B"}}},
	}, Coverage: generator.Coverage{Issues: []string{"uncertain page"}}, Artifact: &generator.Artifact{Version: generator.PipelineVersion, Response: "original model response"}}
	svc := usecase.NewSetService(q, usecase.NewUserService(q))
	for run := 0; run < 2; run++ {
		set, err := svc.SaveGeneratedSet(t.Context(), uuid.New(), "Lecture", r, nil)
		if err != nil || set.CardsTotal != 7 {
			t.Fatalf("%+v %v", set, err)
		}
	}
	if len(facts) != 4 || len(saved) != 14 {
		t.Fatalf("facts=%d cards=%d", len(facts), len(saved))
	}
	ids := map[pgtype.UUID]bool{}
	for i, f := range facts {
		if !f.ID.Valid || uuid.UUID(f.ID.Bytes).Version() != 7 || ids[f.ID] {
			t.Fatalf("invalid or reused UUIDv7 %v", f.ID)
		}
		if i > 0 && bytes.Compare(facts[i-1].ID.Bytes[:], f.ID.Bytes[:]) >= 0 {
			t.Fatal("fact UUIDs do not preserve generation order")
		}
		ids[f.ID] = true
	}
	for i, c := range saved {
		if c.QuestionSvg.String != `<svg viewBox="0 0 1 1"/>` || !c.QuestionSvg.Valid || c.ExplanationSvg.String != `<svg viewBox="0 0 2 2"/>` || !c.ExplanationSvg.Valid {
			t.Fatal("lost generated illustrations")
		}
		if !c.ID.Valid || uuid.UUID(c.ID.Bytes).Version() != 7 {
			t.Fatalf("invalid card UUIDv7 %v", c.ID)
		}
		if i > 0 && bytes.Compare(saved[i-1].ID.Bytes[:], c.ID.Bytes[:]) >= 0 {
			t.Fatal("card UUIDs do not preserve generation order")
		}
	}
	for i, c := range saved {
		factIndex := (i / 7) * 2
		if i%7 >= 5 {
			factIndex++
		}
		if c.FactID != facts[factIndex].ID {
			t.Fatal("crossed fact references")
		}
	}
	for i, source := range []string{"Flip source", "Boolean source", "Choice source", "Table source", "Input source"} {
		if saved[i].Explanation != source || saved[i].SourceRef.String != "Lecture, стр. 2, 3" {
			t.Fatalf("lost card source/order: %+v", saved[i])
		}
	}
	if saved[0].SourceQuote != "Quote 1" || saved[4].SourceQuote != "Quote 5" || saved[1].AnswerText.String != "false" || saved[2].AnswerText.String != `["A","B"]` || saved[3].AnswerText.Valid || saved[4].AnswerText.String != "Different answer" {
		t.Fatal("lost answer or quote")
	}
	if len(options) != 6 || !options[0].IsCorrect || !options[1].IsCorrect || options[2].IsCorrect || len(columns) != 4 || len(items) != 4 || items[0].ColumnName != "X  " || items[1].ItemText != "row Y" {
		t.Fatal("lost choice/table content")
	}
	if items[0].ItemText != "    $\\frac{1}{2}$  \n" {
		t.Fatalf("changed GFM whitespace: %q", items[0].ItemText)
	}
	if len(q.reports) != 2 {
		t.Fatal("missing private reports")
	}
	var report generator.Result
	if err := json.Unmarshal(q.reports[0].Report, &report); err != nil {
		t.Fatal(err)
	}
	if report.Artifact.Response != "original model response" || !reflect.DeepEqual(report.Coverage.Issues, r.Coverage.Issues) || len(report.Facts[0].Cards) != 5 {
		t.Fatal("lost report")
	}
}
