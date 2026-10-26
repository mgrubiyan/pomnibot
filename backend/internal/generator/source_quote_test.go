package generator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestQuotesArePreservedWithoutTextMatching(t *testing.T) {
	for _, tc := range []struct {
		name  string
		doc   Document
		quote string
	}{
		{"text", Document{Text: "A and B"}, "A and B"},
		{"paraphrase", Document{Text: "A and B"}, "B and A"},
		{"wrong page", Document{Text: "A and Bother", PageStarts: []int{0, 7}}, "other"},
		{"block", Document{Structure: &material.Document{Blocks: []material.Block{{Page: 1, Text: "A and B"}}}}, "A and B"},
		{"wrong block page", Document{Structure: &material.Document{Blocks: []material.Block{{Page: 2, Text: "A and B"}}}}, "A and B"},
		{"PDF without text blocks", pdfDocument(), "A and B"},
		{"exact characters", Document{Text: "unrelated"}, "  Цитата: x² ≠ x\n\t "},
		{"empty quote", pdfDocument(), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := learningFact("F")
			// Each pair shares its exact source and quote, including whitespace and Unicode.
			f.Cards[0] = FlipCard{CardBase{Question: "Recall", Source: "Input source", SourceQuote: tc.quote}, "answer"}
			published := 0
			r, err := NewGenerator(stageFixture(f), Options{OnBatch: func(b Batch) {
				published++
				if len(b.Facts) != 1 || b.Facts[0].Cards[1].Base().SourceQuote != tc.quote {
					t.Fatal("lost quote in progress batch")
				}
			}}).Generate(context.Background(), tc.doc)
			if err != nil {
				t.Fatal(err)
			}
			if published != 1 || r.Artifact.Facts[0].Cards[1].Base().SourceQuote != tc.quote || r.Facts[0].Cards[1].(InputCard).SourceQuote != tc.quote || r.Facts[0].Cards[0].(FlipCard).Source != "Input source" {
				t.Fatal("lost shared source")
			}
		})
	}
}
func TestMalformedResponseRetainedWithoutPublishing(t *testing.T) {
	raw := `{"figures":[],"facts":[`
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
func TestFactOrderAndRecallArePreserved(t *testing.T) {
	a, b := learningFact("B"), learningFact("A")
	b.Cards[0] = FlipCard{cardBase("Different question"), "Different answer"}
	r, err := NewGenerator(stageFixture(a, b), Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || len(r.Facts) != 2 || r.Facts[0].Name != "B" || r.Facts[1].Cards[1].(InputCard).Answer != "Different answer" {
		t.Fatalf("%+v %v", r, err)
	}
}
