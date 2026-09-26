package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"testing"
)

// fakeOCR answers every request with the pages it was given for it, or
// makes up a clean page per requested page.
type fakeOCR struct {
	answer func(n int, req OCRRequest) ([]OCRPage, error)
	reqs   []OCRRequest
}

func (f *fakeOCR) Recognize(_ context.Context, req OCRRequest) ([]OCRPage, error) {
	f.reqs = append(f.reqs, req)
	if f.answer != nil {
		return f.answer(len(f.reqs), req)
	}
	pages := make([]OCRPage, req.Pages)
	for i := range pages {
		pages[i] = OCRPage{Text: fmt.Sprintf("Распознанная страница %d. Мейоз — деление, при котором число хромосом уменьшается вдвое.", i+1)}
	}
	return pages, nil
}

func newOCRExtractor(t *testing.T, ocr OCR, pdf PDFText) *Extractor {
	t.Helper()
	e, err := New(ocr, Options{PDF: pdf})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExtractTextLayerSkipsOCR(t *testing.T) {
	ocr := &fakeOCR{}
	res, err := newOCRExtractor(t, ocr, &fakePDF{pages: []string{page(1), page(2)}}).
		Extract(context.Background(), []File{{Name: "typed.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(ocr.reqs) != 0 || res.Source != SourceText {
		t.Errorf("OCR called %d times, source %s; want no OCR for a typed PDF", len(ocr.reqs), res.Source)
	}
}

func TestExtractScannedPDFGoesToOCR(t *testing.T) {
	ocr := &fakeOCR{}
	res, err := newOCRExtractor(t, ocr, &fakePDF{pages: []string{"", " 2 ", ""}}).
		Extract(context.Background(), []File{{Name: "scan.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(ocr.reqs) != 1 {
		t.Fatalf("OCR called %d times, want once for the whole PDF", len(ocr.reqs))
	}
	req := ocr.reqs[0]
	if req.MimeType != "application/pdf" || req.Model != ModelScan || req.Pages != 3 || !bytes.Equal(req.Data, pdfBytes) {
		t.Errorf("request = %+v", req)
	}
	if res.Source != SourceOCR || !strings.Contains(res.Text, "Распознанная страница 3") {
		t.Errorf("result = %+v", res)
	}
	for _, p := range res.Pages {
		if p.Source != SourceOCR || !p.NeedsOCR || p.Poor {
			t.Errorf("page %+v", p)
		}
	}
}

func TestExtractMixedPDF(t *testing.T) {
	ocr := &fakeOCR{}
	res, err := newOCRExtractor(t, ocr, &fakePDF{pages: []string{page(1), "", page(3)}}).
		Extract(context.Background(), []File{{Name: "mixed.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if res.Source != SourceMixed {
		t.Fatalf("source = %s, want mixed", res.Source)
	}
	got := []Source{res.Pages[0].Source, res.Pages[1].Source, res.Pages[2].Source}
	if got[0] != SourceText || got[1] != SourceOCR || got[2] != SourceText {
		t.Errorf("page sources = %v", got)
	}
	// Text layer where there is one, OCR only for the scanned page.
	if strings.Contains(res.Text, "Распознанная страница 1") || !strings.Contains(res.Text, "Распознанная страница 2") ||
		!strings.Contains(res.Text, "Страница 1.") || !strings.Contains(res.Text, "Страница 3.") {
		t.Errorf("text = %q", res.Text)
	}
	if strings.Index(res.Text, "Страница 1.") > strings.Index(res.Text, "Распознанная страница 2") {
		t.Error("pages out of order")
	}
}

func TestExtractPhotosInOrder(t *testing.T) {
	ocr := &fakeOCR{answer: func(n int, _ OCRRequest) ([]OCRPage, error) {
		return []OCRPage{{Text: fmt.Sprintf("Фото номер %d: митоз — непрямое деление соматических клеток.", n)}}, nil
	}}
	files := []File{
		{Name: "IMG_1.jpg", Data: jpegBytes(t, 40, 30)},
		{Name: "IMG_2.png", Data: pngBytes(t, 40, 30)},
		{Name: "IMG_3.jpg", Data: jpegBytes(t, 40, 30)},
	}
	res, err := newOCRExtractor(t, ocr, &fakePDF{}).Extract(context.Background(), files)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(ocr.reqs) != 3 {
		t.Fatalf("OCR called %d times, want once per photo", len(ocr.reqs))
	}
	for i, req := range ocr.reqs {
		if req.Model != ModelPhoto || req.Pages != 1 {
			t.Errorf("request %d = model %q, pages %d", i, req.Model, req.Pages)
		}
	}
	if ocr.reqs[1].MimeType != "image/png" || ocr.reqs[0].MimeType != "image/jpeg" {
		t.Errorf("mime types = %q, %q", ocr.reqs[0].MimeType, ocr.reqs[1].MimeType)
	}
	i1, i2, i3 := strings.Index(res.Text, "Фото номер 1"), strings.Index(res.Text, "Фото номер 2"), strings.Index(res.Text, "Фото номер 3")
	if i1 < 0 || i2 < i1 || i3 < i2 || res.Source != SourceOCR || len(res.Pages) != 3 {
		t.Errorf("text %q, source %s", res.Text, res.Source)
	}
}

func TestExtractOCRModelOverride(t *testing.T) {
	ocr := &fakeOCR{}
	e, err := New(ocr, Options{PDF: &fakePDF{pages: []string{""}}, OCRModel: "page-column-sort"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Extract(context.Background(), []File{{Name: "a.jpg", Data: jpegBytes(t, 8, 8)}, {Name: "b.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	for _, req := range ocr.reqs {
		if req.Model != "page-column-sort" {
			t.Errorf("model = %q, want the override", req.Model)
		}
	}
}

func TestExtractPDFOverPageLimit(t *testing.T) {
	pages := make([]string, MaxOCRPages+1)
	ocr := &fakeOCR{}
	_, err := newOCRExtractor(t, ocr, &fakePDF{pages: pages}).
		Extract(context.Background(), []File{{Name: "book.pdf", Data: pdfBytes}})
	var ie *Error
	if !errors.As(err, &ie) || !errors.Is(err, ErrTooManyPages) || !strings.Contains(ie.Message, "200") {
		t.Fatalf("error = %v, want ErrTooManyPages naming the limit", err)
	}
	if len(ocr.reqs) != 0 {
		t.Error("the service was called for a PDF over its page limit")
	}
}

func TestExtractPDFOverSizeLimit(t *testing.T) {
	e := newOCRExtractor(t, &fakeOCR{}, &fakePDF{pages: []string{"", ""}})
	e.lim.bytes = len(pdfBytes) - 1
	_, err := e.Extract(context.Background(), []File{{Name: "big.pdf", Data: pdfBytes}})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestExtractWarnsAboutPoorPages(t *testing.T) {
	ocr := &fakeOCR{answer: func(n int, _ OCRRequest) ([]OCRPage, error) {
		if n == 2 {
			return []OCRPage{{Text: "¤¤ ▒▓ ░░ ¦¦ ◊◊ ¤▒ ▓░ ◊¦ ¤¤ ▒▓ ░░ ¦¦ ◊◊ ¤▒"}}, nil
		}
		return []OCRPage{{Text: "Митоз — непрямое деление соматических клеток, при котором образуются две дочерние."}}, nil
	}}
	res, err := newOCRExtractor(t, ocr, &fakePDF{}).Extract(context.Background(), []File{
		{Name: "1.jpg", Data: jpegBytes(t, 8, 8)},
		{Name: "2.jpg", Data: jpegBytes(t, 8, 8)},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if res.Pages[0].Poor || !res.Pages[1].Poor {
		t.Errorf("pages = %+v, want only page 2 poor", res.Pages)
	}
	want := "Страница 2 распознана плохо, попробуйте переснять."
	if len(res.Warnings) != 1 || res.Warnings[0] != want {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
	// The text is kept as recognized, not cleaned up.
	if !strings.Contains(res.Text, "¤¤ ▒▓") {
		t.Error("the poorly recognized text was altered")
	}
}

func TestExtractOCRFailure(t *testing.T) {
	ocr := &fakeOCR{answer: func(int, OCRRequest) ([]OCRPage, error) { return nil, errors.New("503") }}
	_, err := newOCRExtractor(t, ocr, &fakePDF{}).Extract(context.Background(), []File{{Name: "a.jpg", Data: jpegBytes(t, 8, 8)}})
	var ie *Error
	if !errors.As(err, &ie) || !errors.Is(err, ErrOCR) || ie.Message == "" {
		t.Errorf("error = %v, want *Error with ErrOCR", err)
	}
}

func TestExtractShrinksLargePhotos(t *testing.T) {
	ocr := &fakeOCR{}
	e := newOCRExtractor(t, ocr, &fakePDF{})
	e.lim.pixels = 1000

	small, large := pngBytes(t, 20, 20), pngBytes(t, 200, 100)
	if _, err := e.Extract(context.Background(), []File{{Name: "s.png", Data: small}, {Name: "l.png", Data: large}}); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !bytes.Equal(ocr.reqs[0].Data, small) || ocr.reqs[0].MimeType != "image/png" {
		t.Error("a photo within the limits was changed")
	}
	sent := ocr.reqs[1]
	cfg, format, err := image.DecodeConfig(bytes.NewReader(sent.Data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || sent.MimeType != "image/jpeg" || cfg.Width*cfg.Height > 1000 || cfg.Width < cfg.Height {
		t.Errorf("sent %s %dx%d (%s), want a JPEG within 1000 pixels keeping the proportions", format, cfg.Width, cfg.Height, sent.MimeType)
	}
}
