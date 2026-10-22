package generator

import (
	"context"
	"slices"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestPlanningBindsScopeToOmittedPageHeading(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "heading", Page: 4, Kind: "heading", Text: "Плотный индекс"},
		{ID: "rule", Page: 4, Kind: "text", Text: "Поиск выполняется методом бинарного поиска по блокам."},
	}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"topics":[{"id":"dense","title":"Плотный индекс","blockIds":["rule"],"objectives":[{"id":"search","text":"Способ поиска","blockIds":["rule"],"scope":["плотный индекс"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 {
		t.Fatalf("source heading should not require another model call: %d", len(p.requests))
	}
	topic := plan.Topics[0]
	if !slices.Contains(topic.BlockIDs, "heading") || !slices.Contains(topic.Objectives[0].BlockIDs, "heading") {
		t.Fatalf("heading missing from topic or objective: %+v", topic)
	}
	bindObjectiveScopes(&plan, d)
	if len(plan.Topics[0].BlockIDs) != 2 || len(plan.Topics[0].Objectives[0].BlockIDs) != 2 {
		t.Fatal("rebinding duplicated source blocks")
	}
}

func TestScopeBindingRejectsUnrelatedOrUnobservedHeadings(t *testing.T) {
	for _, tt := range []struct {
		name   string
		blocks []material.Block
	}{
		{"different page", []material.Block{
			{ID: "heading", Page: 3, Kind: "heading", Text: "Плотный индекс"},
			{ID: "rule", Page: 4, Text: "Бинарный поиск."},
		}},
		{"following heading", []material.Block{
			{ID: "rule", Page: 4, Text: "Бинарный поиск."},
			{ID: "heading", Page: 4, Kind: "heading", Text: "Плотный индекс"},
		}},
		{"different section", []material.Block{
			{ID: "heading", Page: 4, Kind: "heading", Text: "Плотный индекс"},
			{ID: "other", Page: 4, Kind: "heading", Text: "Неплотный индекс"},
			{ID: "rule", Page: 4, Text: "Бинарный поиск."},
		}},
		{"visual interpretation", []material.Block{
			{ID: "heading", Page: 4, Kind: "heading", Observations: "Плотный индекс"},
			{ID: "rule", Page: 4, Text: "Бинарный поиск."},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := material.Document{Blocks: tt.blocks}
			p := Plan{Topics: []Topic{{ID: "t", Title: "Поиск", BlockIDs: []string{"rule"}, Objectives: []Objective{{ID: "o", Text: "Способ поиска", BlockIDs: []string{"rule"}, Scope: []string{"плотный индекс"}}}}}}
			bindObjectiveScopes(&p, d)
			if validatePlan(p, d) == nil {
				t.Fatal("scope accepted without a heading belonging to the source rule")
			}
		})
	}
}
