package notes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type directProvider func(context.Context, providers.Request) (providers.Response, error)

func (f directProvider) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return f(ctx, r)
}

type captureDirectSaver struct {
	result generator.Result
	calls  int
}

func (s *captureDirectSaver) SaveGeneratedSet(_ context.Context, _ int64, title string, r generator.Result) (*contracts.CardSet, error) {
	s.result = r
	s.calls++
	return &contracts.CardSet{Title: title}, nil
}

func TestMakeWithDirectGeneratorPreservesProgressAndSavePayload(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			text := "Индекс ускоряет поиск записей по ключу."
			d := material.Document{Blocks: []material.Block{{ID: "s", Page: 1, Text: text}}}
			ex := &fakeExtractor{result: ingest.Result{Text: text, Document: d}}
			calls := 0
			model := directProvider(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
				finish, err := providers.ReserveCall(ctx, .01)
				if err != nil {
					return providers.Response{}, err
				}
				calls++
				usage := providers.Usage{CostUSD: .001}
				finish(usage, true)
				if failure {
					return providers.Response{}, errors.New("unavailable")
				}
				goal := `{"goalId":"g1","name":"Назначение","topic":"Индекс","pages":[1],"source":"` + text + `"}`
				group := `{"goalId":"g1","content":{"name":"Назначение","question":"Для чего нужен индекс?","answer":"Для ускорения поиска по ключу.","cards":[]}}`
				content := `{"issues":[],"goals":[` + goal + `],"cards":[` + group + `]}`
				return providers.Response{Usage: usage, Content: []byte(content)}, nil
			})
			saver := &captureDirectSaver{}
			maker := New(ex, FromGenerator(generator.NewGenerator(model, generator.Options{})), saver, 1).WithBudget(providers.BudgetLimits{USD: .05, Calls: 3})
			progress := 0
			generating := false
			result, err := maker.Make(context.Background(), Request{UserID: 42, Files: file("lecture.pdf")}, Progress{Generating: func() { generating = true }, Cards: func(n int) { progress = n }})
			if failure {
				if err == nil || saver.calls != 0 || progress != 0 {
					t.Fatal("failed result saved or published")
				}
				return
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("obsolete coverage warning: %v", result.Warnings)
			}
			r := saver.result
			if err != nil || calls != 1 || saver.calls != 1 || !generating || progress != 2 || len(r.Facts) != 1 || len(r.Cards) != 2 || r.Cards[0].FactID != r.Facts[0].ID || r.Budget.Calls != 1 || r.Budget.ReportedUSD != .001 {
				t.Fatalf("lost save/progress/ledger: %+v err=%v", r, err)
			}
			if r.Artifact == nil || r.Artifact.Version != generator.PipelineVersion || len(r.Artifact.Files) != 1 || r.Artifact.Reading.Blocks[0].Text != text {
				t.Fatal("lost private original")
			}
		})
	}
}
func TestFailureTextForWholeSourceLimit(t *testing.T) {
	got := FailureText(fmt.Errorf("generate: %w", providers.ErrCapability))
	if got == TextTryLater || got == "" {
		t.Fatal("source limit tells user to retry unchanged document")
	}
}

func TestMakeSurfacesSourceUncertainty(t *testing.T) {
	gen := &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		r := oneCard("T")
		r.Coverage.Issues = []string{"Страница 2: неразборчивое условие пропущено"}
		return r, nil
	}}
	ex := &fakeExtractor{result: ingest.Result{Text: "Читаемая часть"}}
	saver := &captureDirectSaver{}
	r, err := New(ex, gen, saver, 1).Make(context.Background(), Request{Files: file("notes.txt")}, Progress{})
	if err != nil || saver.calls != 1 || len(r.Warnings) != 1 || r.Warnings[0] != "Страница 2: неразборчивое условие пропущено" {
		t.Fatalf("%+v %v", r, err)
	}
}
