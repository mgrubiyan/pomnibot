package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

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
