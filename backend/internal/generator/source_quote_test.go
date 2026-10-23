package generator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestQuotesMustBeVerbatimOnCitedTextPages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		doc   Document
		quote string
		valid bool
	}{
		{"text", Document{Text: "A and B"}, "A and B", true},
		{"paraphrase", Document{Text: "A and B"}, "B and A", false},
		{"wrong page", Document{Text: "A and Bother", PageStarts: []int{0, 7}}, "other", false},
		{"block", Document{Structure: &material.Document{Blocks: []material.Block{{ID: "p1", Page: 1, Text: "A and B"}}}}, "A and B", true},
		{"wrong block page", Document{Structure: &material.Document{Blocks: []material.Block{{ID: "p2", Page: 2, Text: "A and B"}}}}, "A and B", false},
		{"PDF cannot verify text quote", pdfDocument(), "A and B", false},
		{"PDF retains references", pdfDocument(), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goal := learningGoal("model-id", "F")
			goal.SourceQuote = tc.quote
			groups := []goalCards{{GoalID: goal.GoalID, Content: requiredFormatsFixture()}}
			r, err := NewGenerator(stageFixture([]KnowledgeGoal{goal}, groups), Options{}).Generate(context.Background(), tc.doc)
			if !tc.valid {
				if err == nil || len(r.Cards) != 0 || len(r.Facts) != 0 {
					t.Fatalf("%+v %v", r, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range r.Cards {
				if c.SourceQuote != tc.quote || c.Explanation != goal.Source || !strings.Contains(c.SourceRef, "1") {
					t.Fatalf("lost source: %+v", c)
				}
			}
		})
	}
}
func TestMalformedResponseRetainedWithoutPublishing(t *testing.T) {
	raw := `{"goals":[`
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(raw), Truncated: true}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err == nil || r.Artifact.Response != raw || len(r.Cards) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = json.Marshal(r); err != nil {
		t.Fatal("malformed response broke result serialization", err)
	}
}
func TestIndependentGoalsHaveIndependentProgress(t *testing.T) {
	goals := []KnowledgeGoal{learningGoal("g2", "A"), learningGoal("g1", "B")}
	groups := []goalCards{}
	for _, g := range goals {
		f := Fact{Name: g.Name, Cards: []Card{FlipCard{CardBase{g.Name + "?"}, "answer"}, InputCard{CardBase{g.Name + "?"}, "answer"}}}
		groups = append(groups, goalCards{GoalID: g.GoalID, Content: f})
	}
	// Return groups in a different order and use IDs colliding with canonical IDs.
	groups[0], groups[1] = groups[1], groups[0]
	r, err := NewGenerator(stageFixture(goals, groups), Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || len(r.Facts) != 2 || len(r.Cards) != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Facts[0].ID == r.Facts[1].ID || r.Facts[0].Topic != r.Facts[1].Topic {
		t.Fatal("lost independent progress")
	}
	for i, c := range r.Cards {
		if c.FactID != r.Facts[i/2].ID || c.Question != goals[i/2].Name+"?" {
			t.Fatal("crossed goal references")
		}
	}
}

func TestCommonQuestionAndAnswerProduceMandatoryPair(t *testing.T) {
	raw := `{"issues":[],"goals":[` + testGoal + `],"cards":[{"goalId":"local-a","content":` + testContent + `}]}`
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(raw)}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || len(r.Cards) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Cards[0].Question != r.Cards[1].Question || r.Cards[0].Answer != r.Cards[1].Answer {
		t.Fatal("mandatory pair diverged")
	}
}
