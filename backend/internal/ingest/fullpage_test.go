package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type pageModelFunc func(context.Context, providers.Request) (providers.Response, error)

func (f pageModelFunc) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	return f(ctx, req)
}

func TestFullPDFPagesPreserveLayoutAndDiscardInterpretations(t *testing.T) {
	texts := []string{page(1), "", "Таблица: sin 30° = 1/2"}
	var rendered []int
	calls := 0
	model := pageModelFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		if req.Image == nil || !json.Valid(req.Schema) || !strings.Contains(req.User, texts[calls]) {
			t.Fatalf("missing full page, schema or reference on page %d", calls)
		}
		if calls == 0 && !strings.Contains(req.User, "Текстовый слой") {
			t.Error("no text layer reference")
		}
		out, _ := json.Marshal(map[string]any{"text": texts[calls], "observations": "Предположительно показан закон Ньютона."})
		calls++
		return providers.Response{Content: out}, nil
	})
	ocr := &VisionOCR{Model: model, FullPage: true, Render: func(_ context.Context, _ []byte, n int) ([]byte, error) {
		rendered = append(rendered, n)
		return jpegOf(t, 1200, 1600), nil
	}}
	ex, err := NewExtractor(ocr, Options{PDF: &fakePDF{pages: append([]string(nil), texts...)}, ReadAllPDFPages: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ex.Extract(context.Background(), []File{{Name: "slides.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !reflect.DeepEqual(rendered, []int{0, 1, 2}) {
		t.Fatalf("calls=%d pages=%v", calls, rendered)
	}
	if strings.Contains(res.Text, "Ньютона") || !strings.Contains(res.Text, texts[2]) {
		t.Fatalf("interpretations leaked or table lost: %s", res.Text)
	}
	if len(res.Pages) != 3 || res.Pages[2].Page != 3 || res.Pages[2].Number != 3 || res.Pages[2].Start <= 0 || !strings.HasPrefix(res.Text[res.Pages[2].Start:], texts[2]) {
		t.Fatalf("pages=%+v", res.Pages)
	}
}

func TestFullPageFailureKeepsReferenceOrChecker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response providers.Response
		err      error
	}{
		{name: "provider failure", err: errors.New("offline")},
		{name: "invalid json", response: providers.Response{Content: []byte("invented text")}},
		{name: "truncated", response: providers.Response{Content: []byte(`{"text":"Invented"}`), Truncated: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := pageModelFunc(func(context.Context, providers.Request) (providers.Response, error) { return tc.response, tc.err })
			ocr := &VisionOCR{FullPage: true, Model: model, Render: func(context.Context, []byte, int) ([]byte, error) { return jpegOf(t, 600, 800), nil }}
			res, err := ocr.Recognize(context.Background(), OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Pages: 1, TextLayers: []string{page(1)}})
			if err != nil || len(res) != 1 || res[0].Text != page(1) {
				t.Fatalf("reference fallback: %+v, %v", res, err)
			}
			ocr.Checker = &fakeChecker{text: page(2)}
			res, err = ocr.Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
			if err != nil || res[0].Text != page(2) {
				t.Fatalf("checker fallback: %+v, %v", res, err)
			}
		})
	}
}

func TestFullPageReadsWholeImageAndFlagsDisagreement(t *testing.T) {
	model := &fakeModel{texts: []string{`{"text":"Митоз — непрямое деление клеток.\nВыдуманное описание планет солнечной системы.","observations":"Схема деления."}`}}
	ocr := &VisionOCR{Model: model, FullPage: true, Checker: &fakeChecker{text: "Митоз — непрямое деление клеток."}}
	res, err := ocr.Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 1200, 1600), MimeType: "image/jpeg", Pages: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.sizes) != 1 || model.sizes[0].Y != 1600 || !strings.Contains(res[0].Text, "планет") || len(res[0].Issues) == 0 || res[0].HasConfidence || strings.Contains(res[0].Text, "Схема") {
		t.Fatalf("sizes=%v pages=%+v", model.sizes, res)
	}
}

func TestFullPageCancellationDoesNotUseReference(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := pageModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		cancel()
		return providers.Response{}, context.Canceled
	})
	ocr := &VisionOCR{FullPage: true, Model: model, Render: func(context.Context, []byte, int) ([]byte, error) { return jpegOf(t, 600, 800), nil }}
	_, err := ocr.Recognize(ctx, OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Pages: 1, TextLayers: []string{page(1)}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestFullPagesDoNotReduceExistingTextPDFLimits(t *testing.T) {
	ocr := &fakeOCR{}
	ex, err := NewExtractor(ocr, Options{PDF: &fakePDF{pages: []string{page(1), page(2), page(3)}}, ReadAllPDFPages: true})
	if err != nil {
		t.Fatal(err)
	}
	ex.lim.pages = 2
	res, err := ex.Extract(context.Background(), []File{{Name: "long.pdf", Data: pdfBytes}})
	if err != nil || len(ocr.reqs) != 0 || len(res.Pages) != 3 || !strings.Contains(res.Text, strings.TrimSpace(page(3))) {
		t.Fatalf("text-only limit regressed: %v, %+v", err, res.Pages)
	}
}

func TestFullPageRenderFailureKeepsTextLayer(t *testing.T) {
	ocr := &VisionOCR{FullPage: true, Render: func(context.Context, []byte, int) ([]byte, error) { return nil, errors.New("render failed") }}
	res, err := ocr.Recognize(context.Background(), OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Pages: 1, TextLayers: []string{page(1)}})
	if err != nil || len(res) != 1 || res[0].Text != page(1) || !res[0].FromTextLayer {
		t.Fatalf("text layer lost: %+v %v", res, err)
	}
}

func TestFullPagePDFWithPoppler(t *testing.T) {
	model := &fakeModel{texts: []string{`{"text":"sin 30 = 1/2","observations":""}`}}
	ex, err := NewExtractor(&VisionOCR{Model: model, FullPage: true}, Options{ReadAllPDFPages: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ex.Extract(context.Background(), []File{{Name: "lecture.pdf", Data: minimalPDF("sin 30 = 1/2")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.sizes) != 1 || len(res.Pages) != 1 || !strings.Contains(res.Text, "sin 30") {
		t.Fatalf("result=%+v sizes=%v", res, model.sizes)
	}
}

func TestFullPageDoesNotPromoteAdmittedReconstructionToSource(t *testing.T) {
	model := &fakeModel{texts: []string{`{"text":"SELECT x FROM t","observations":"","issues":["Обрезанные слова восстановлены по стандартному синтаксису"],"blocks":[]}`}}
	reader := &VisionOCR{Model: model, FullPage: true}
	p, err := reader.readFullPage(context.Background(), jpegOf(t, 600, 800), "image/jpeg", ModelScan, "ELECT x\nROM t")
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != "ELECT x\nROM t" || len(p.Issues) == 0 || strings.Contains(p.Text, "SELECT") {
		t.Fatalf("reconstruction presented as transcription: %q issues=%v", p.Text, p.Issues)
	}
}

func TestMissingPageNumberDoesNotDuplicateAllSourceBlocks(t *testing.T) {
	model := &fakeModel{texts: []string{`{"text":"Определение\nКлюч однозначно определяет строку.\n17","observations":"","issues":[],"blocks":[{"kind":"heading","text":"Определение"},{"kind":"text","text":"Ключ однозначно определяет строку."}]}`}}
	p, err := (&VisionOCR{Model: model, FullPage: true}).readFullPage(context.Background(), jpegOf(t, 600, 800), "image/jpeg", ModelScan, "")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, b := range p.Blocks {
		if strings.Contains(b.Text, "Ключ однозначно") {
			count++
		}
	}
	if count != 1 || len(p.Issues) != 0 {
		t.Fatalf("source duplicated for omitted page number: count=%d issues=%v", count, p.Issues)
	}
}
