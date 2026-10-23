package generator

import (
	"context"
	"encoding/json"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"testing"
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
		{"block", Document{Structure: &material.Document{Blocks: []material.Block{{Page: 1, Text: "A and B"}}}}, "A and B", true},
		{"wrong block page", Document{Structure: &material.Document{Blocks: []material.Block{{Page: 2, Text: "A and B"}}}}, "A and B", false},
		{"PDF cannot verify quote", pdfDocument(), "A and B", false},
		{"PDF references without quote", pdfDocument(), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := learningFact("F")
			// Only the input has a quote: each card must be checked independently.
			f.Cards[1] = InputCard{CardBase{Question: "Recall", Source: "Input source", SourceQuote: tc.quote}, "answer"}
			r, err := NewGenerator(stageFixture(f), Options{OnBatch: func(Batch) {
				if !tc.valid {
					t.Fatal("published invalid quote")
				}
			}}).Generate(context.Background(), tc.doc)
			if !tc.valid {
				if err == nil || len(r.Facts) != 0 {
					t.Fatalf("%+v %v", r, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Facts[0].Cards[1].(InputCard).SourceQuote != tc.quote || r.Facts[0].Cards[0].(FlipCard).Source == "Input source" {
				t.Fatal("lost individual source")
			}
		})
	}
}
func TestMalformedResponseRetainedWithoutPublishing(t *testing.T) {
	raw := `{"facts":[`
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(raw), Truncated: true}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err == nil || r.Artifact.Response != raw || len(r.Facts) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = json.Marshal(r); err != nil {
		t.Fatal(err)
	}
}
func TestFactOrderAndDifferentMandatoryPairArePreserved(t *testing.T) {
	a, b := learningFact("B"), learningFact("A")
	b.Cards[1] = InputCard{cardBase("Different question"), "Different answer"}
	r, err := NewGenerator(stageFixture(a, b), Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || len(r.Facts) != 2 || r.Facts[0].Name != "B" || r.Facts[1].Cards[1].(InputCard).Answer != "Different answer" {
		t.Fatalf("%+v %v", r, err)
	}
}
