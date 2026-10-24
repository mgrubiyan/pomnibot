package ingest

import (
	"context"
	"errors"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"testing"
	"time"
)

func TestRenderFailureCannotBecomeReusableReading(t *testing.T) {
	model := pageModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		t.Fatal("model must not be called without a rendered image")
		return providers.Response{}, nil
	})
	v := VisionOCR{Model: model, FullPage: true, Render: func(context.Context, []byte, int) ([]byte, error) {
		return nil, errors.New("renderer failed")
	}}
	reference := "В SQLite действие по умолчанию — NO ACTION."
	pages, err := v.Recognize(context.Background(), OCRRequest{MimeType: "application/pdf", Pages: 1, TextLayers: []string{reference}})
	if err != nil || len(pages) != 1 || pages[0].Text != reference {
		t.Fatalf("fallback lost: %+v %v", pages, err)
	}
	r := Result{Pages: []PageInfo{{ReadFailed: pages[0].ReadFailed, Issues: pages[0].Issues}}}
	if ReusableReading(r) {
		t.Fatal("failed required image render was accepted as a successful reading")
	}
}

func TestReadingReuseDistinguishesSourceUncertaintyFromFailure(t *testing.T) {
	r := Result{Pages: []PageInfo{{Issues: []string{"Источник обрезан; условие не восстановлено."}}}}
	if !ReusableReading(r) {
		t.Fatal("successful reading with preserved source limitations should survive generator changes")
	}
	r.Pages[0].ReadFailed = true
	if ReusableReading(r) {
		t.Fatal("fallback reading cached as complete")
	}
}

func TestSparseStructuredDiagramIsNotMistakenForFailedReading(t *testing.T) {
	r := Result{Pages: []PageInfo{{Poor: true, Image: []byte("original image"), Blocks: []material.Block{{Kind: "diagram", Text: "A → B"}}}}}
	if !ReusableReading(r) {
		t.Fatal("short but successfully structured visual reading is repeated")
	}
	r.Pages[0].Blocks = nil
	if ReusableReading(r) {
		t.Fatal("poor unstructured OCR cached")
	}
}

func TestPageTimeoutPreservesFallbackAndMarksReadingIncomplete(t *testing.T) {
	model := pageModelFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		<-ctx.Done()
		return providers.Response{}, ctx.Err()
	})
	v := VisionOCR{Model: model, FullPage: true, CallTimeout: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reference := "Сохрани исходный текст и его условия."
	page, err := v.readFullPage(ctx, jpegOf(t, 100, 100), "image/jpeg", ModelScan, reference)
	if err != nil || page.Text != reference || !page.ReadFailed || ctx.Err() != nil {
		t.Fatalf("page timeout lost fallback or canceled document: %+v %v", page, err)
	}
}
