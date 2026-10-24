package ingest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestBuildDocumentKeepsPageAndBlockIssuesSeparate(t *testing.T) {
	pageIssues := []string{"Page reading is uncertain"}
	for _, tc := range []struct {
		name   string
		blocks []material.Block
	}{
		{name: "paragraph fallback"},
		{name: "native blocks", blocks: []material.Block{
			{Kind: "text", Text: "First paragraph"},
			{Kind: "text", Text: "Second paragraph", Issues: []string{"Block symbol is unclear"}},
			{Kind: "text", Text: "Third paragraph", Issues: pageIssues},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := "First paragraph\n\nSecond paragraph\n\nThird paragraph"
			doc := buildDocument(Result{Text: text, Pages: []PageInfo{{Number: 1, Issues: pageIssues, Blocks: tc.blocks}}})
			if len(doc.Pages) != 1 || doc.Pages[0].Reading != text || !slices.Equal(doc.Pages[0].Issues, pageIssues) {
				t.Fatalf("page text or issues lost: %+v", doc.Pages)
			}
			if len(doc.Blocks) != 3 {
				t.Fatalf("got %d blocks, want 3", len(doc.Blocks))
			}
			for i, block := range doc.Blocks {
				var wantIssues []string
				if len(tc.blocks) > 0 {
					wantIssues = tc.blocks[i].Issues
				}
				if block.Text != strings.Split(text, "\n\n")[i] || !slices.Equal(block.Issues, wantIssues) {
					t.Errorf("block %d: got %+v, want original text and issues %v", i, block, wantIssues)
				}
			}
		})
	}
}

func TestStructuredReadingRetainsImageOnlyContentAndUncertainty(t *testing.T) {
	model := pageModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(`{"text":"Если x > 0, то ln x определён.\nA → B","observations":"Стрелка от A к B.","issues":["Символ x может быть χ"],"blocks":[{"kind":"formula","text":"Если x > 0, то ln x определён.","observations":"","region":[0,0,1,0.5],"cells":[],"issues":["Символ x может быть χ"]},{"kind":"diagram","text":"A → B","observations":"Стрелка от A к B.","region":[0,0.5,1,1],"cells":[],"issues":[]}]}`)}, nil
	})
	ex, err := NewExtractor(&VisionOCR{Model: model, FullPage: true, Render: func(context.Context, []byte, int) ([]byte, error) { return jpegOf(t, 600, 800), nil }}, Options{PDF: &fakePDF{pages: []string{strings.Repeat("Текстовый слой. ", 10)}}, ReadAllPDFPages: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ex.Extract(context.Background(), []File{{Name: "mixed.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "ln x") {
		t.Fatal("image-only formula removed because checker did not see it")
	}
	if len(res.Document.Blocks) != 2 || len(res.Document.Blocks[0].Issues) == 0 || res.Document.Blocks[1].Observations == "" {
		t.Fatalf("lost structure: %+v", res.Document)
	}
	if strings.Contains(res.Text, "Стрелка от") {
		t.Fatal("interpretation turned into quotation")
	}
}
