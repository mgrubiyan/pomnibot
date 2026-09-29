package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"slices"
	"sync"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// fakeModel answers each image with the next of its texts and records the
// sizes of the images it saw.
type fakeModel struct {
	texts []string
	err   error

	mu    sync.Mutex
	sizes []image.Point
}

func (m *fakeModel) Complete(_ context.Context, req providers.Request) (providers.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.Image == nil {
		return providers.Response{}, errors.New("no image")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(req.Image.Data))
	if err != nil {
		return providers.Response{}, err
	}
	m.sizes = append(m.sizes, image.Pt(cfg.Width, cfg.Height))
	if m.err != nil {
		return providers.Response{}, m.err
	}
	n := len(m.sizes) - 1
	if n >= len(m.texts) {
		return providers.Response{}, fmt.Errorf("unexpected call %d", n+1)
	}
	return providers.Response{Content: []byte(m.texts[n])}, nil
}

// fakeChecker plays the classic OCR: noisy text and the rotation it saw.
type fakeChecker struct {
	text   string
	rotate int
	reqs   []OCRRequest
}

func (c *fakeChecker) Recognize(_ context.Context, req OCRRequest) ([]OCRPage, error) {
	c.reqs = append(c.reqs, req)
	return []OCRPage{{Text: c.text, Rotate: c.rotate}}, nil
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A notebook photographed sideways, read as it was live: the model makes
// up a theorem the classic OCR does not back, and repeats the line at the
// seam of the two halves.
func TestVisionReadsUprightHalvesAndDropsUnconfirmedLines(t *testing.T) {
	model := &fakeModel{texts: []string{
		"Билет №19\nТеорема об измерении угла между наклонной и плоскостью.\nПостройте трапецию по четырём сторонам.",
		"Постройте трапецию по четырём сторонам.\nsin 30° = 1/2\nУгол между касательной и хордой измеряется половиной дуги.",
	}}
	checker := &fakeChecker{rotate: 90, text: "Билет No19\nГеоргий об измерении угли меж-\nду насательной и кордой.\n" +
		"3) Постройте трапецию по четырем сторонам.\nsin 3\nУгол между насательной и хордой измеряет-\nся половиной дуги"}
	ocr := &VisionOCR{Model: model, Checker: checker}

	pages, err := ocr.Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 1280, 960), MimeType: "image/jpeg", Model: ModelPhoto, Pages: 1})
	if err != nil || len(pages) != 1 {
		t.Fatalf("Recognize() = %+v, %v", pages, err)
	}
	want := "Билет №19\nПостройте трапецию по четырём сторонам.\nsin 30° = 1/2\nУгол между касательной и хордой измеряется половиной дуги."
	if pages[0].Text != want {
		t.Errorf("text:\n got: %q\nwant: %q", pages[0].Text, want)
	}
	// Turned upright (960 wide, 1280 high) and cut into two halves.
	if len(model.sizes) != 2 || model.sizes[0].X != 960 || model.sizes[0].Y >= 960 {
		t.Errorf("model saw %v, want two halves of an upright page", model.sizes)
	}
	if len(checker.reqs) != 1 || checker.reqs[0].Model != ModelPhoto {
		t.Errorf("checker requests %+v, want the photo once with the photo model", checker.reqs)
	}
	// Two of the three lines it could judge are confirmed.
	if !pages[0].HasConfidence || pages[0].Confidence < 0.6 || pages[0].Confidence > 0.7 {
		t.Errorf("confidence %v (%v), want 2 of 3", pages[0].Confidence, pages[0].HasConfidence)
	}
}

func TestVisionFallsBackToCheckerWhenModelFails(t *testing.T) {
	for _, err := range []error{errors.New("unavailable"), fmt.Errorf("gigachat: %w", providers.ErrRefused)} {
		checker := &fakeChecker{text: "Митоз — непрямое деление клеток."}
		ocr := &VisionOCR{Model: &fakeModel{err: err}, Checker: checker}
		pages, gotErr := ocr.Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
		if gotErr != nil || len(pages) != 1 || pages[0].Text != checker.text {
			t.Errorf("model error %v: got %+v, %v; want the checker's text", err, pages, gotErr)
		}
	}
}

func TestVisionWithoutCheckerTakesModelText(t *testing.T) {
	model := &fakeModel{texts: []string{"Митоз — непрямое деление клеток."}}
	pages, err := (&VisionOCR{Model: model}).Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
	if err != nil || len(pages) != 1 || pages[0].Text != "Митоз — непрямое деление клеток." || pages[0].HasConfidence {
		t.Errorf("got %+v, %v; want the model's text, unchecked", pages, err)
	}
	if len(model.sizes) != 1 {
		t.Errorf("model saw %v, want one image: a small page is not cut", model.sizes)
	}
}

// A mixed PDF: only its scanned pages are rendered and read.
func TestVisionReadsOnlyRequestedPDFPages(t *testing.T) {
	var rendered []int
	model := &fakeModel{texts: []string{"Вторая страница."}}
	ocr := &VisionOCR{Model: model, Render: func(_ context.Context, _ []byte, page int) ([]byte, error) {
		rendered = append(rendered, page)
		return jpegOf(t, 600, 800), nil
	}}
	pages, err := ocr.Recognize(context.Background(), OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Model: ModelScan, Pages: 3, Only: []int{1}})
	if err != nil || len(pages) != 3 {
		t.Fatalf("Recognize() = %+v, %v", pages, err)
	}
	if !slices.Equal(rendered, []int{1}) || pages[1].Text != "Вторая страница." || pages[0].Text != "" || pages[2].Text != "" {
		t.Errorf("rendered %v, pages %+v; want only the second page read", rendered, pages)
	}
}

// Seen live: the model wraps formulas in LaTeX dollars despite the prompt.
func TestVisionDropsLaTeXDollars(t *testing.T) {
	model := &fakeModel{texts: []string{"2. $∠AOK = ◡AK$ — центральный, $$∠AKO = 90° − ◡AK/2$$\nЦена тетради $5"}}
	pages, err := (&VisionOCR{Model: model}).Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
	want := "2. ∠AOK = ◡AK — центральный, ∠AKO = 90° − ◡AK/2\nЦена тетради $5"
	if err != nil || len(pages) != 1 || pages[0].Text != want {
		t.Errorf("got %+v, %v; want %q", pages, err, want)
	}
}

func TestLineConfirmed(t *testing.T) {
	checker := checkWords("Угол между насательной и хордой измеряет-\nся половиной дуги. Георгий об измерении угли меж-\nду")
	for line, want := range map[string]bool{
		"Угол между касательной и хордой измеряется половиной дуги.": true,  // one letter off, word broken over lines
		"Теорема об измерении угла между наклонной и плоскостью.":    false, // made up
		"Постройте треугольник по четырём сторонам.":                 false,
	} {
		if got, judged := lineConfirmed(line, checker); !judged || got != want {
			t.Errorf("lineConfirmed(%q) = %v (judged %v), want %v", line, got, judged, want)
		}
	}
	// Formulas and single words give nothing to judge by.
	for _, line := range []string{"sin 30° = 1/2", "Билет №19", ""} {
		if _, judged := lineConfirmed(line, checker); judged {
			t.Errorf("lineConfirmed(%q) judged, want no verdict", line)
		}
	}
}
