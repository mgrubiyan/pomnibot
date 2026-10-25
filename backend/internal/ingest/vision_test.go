package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os/exec"
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
	// A transcription is not checked: the OCR reads phonetic signs anyhow,
	// and the stress marks ˈ and ˌ count as letters, gluing syllables.
	vocabulary := checkWords("7 vulnerability [,vлln(ə)rə'biləti] – уязвимость")
	for line, want := range map[string]bool{
		"7 vulnerability [ˌvʌln(ə)rəˈbɪləti] – уязвимость":  true,
		"7 vulnerability [ˌvʌln(ə)rəˈbɪləti] – беззащитный": false,
	} {
		if got, judged := lineConfirmed(line, vocabulary); !judged || got != want {
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

// A photo with no notes on it, a bird, comes back as «[неразборчиво]»: the
// mark says nothing, so the page is empty and the bot asks for a better
// photo rather than more detailed notes.
func TestVisionDropsLinesOnlyMarkedIllegible(t *testing.T) {
	for _, text := range []string{"[неразборчиво]", "[неразборчиво]\n[Неразборчиво]\n"} {
		model := &fakeModel{texts: []string{text}}
		pages, err := (&VisionOCR{Model: model}).Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
		if err != nil || len(pages) != 1 || pages[0].Text != "" {
			t.Errorf("%q: got %+v, %v; want an empty page", text, pages, err)
		}
	}
	model := &fakeModel{texts: []string{"Митоз — [неразборчиво] деление клеток."}}
	pages, _ := (&VisionOCR{Model: model}).Recognize(context.Background(), OCRRequest{Data: jpegOf(t, 600, 800), MimeType: "image/jpeg", Pages: 1})
	if len(pages) != 1 || pages[0].Text != "Митоз — [неразборчиво] деление клеток." {
		t.Errorf("got %+v, want a line with a word marked kept", pages)
	}
}

// A page rendered over the limit is refused before the checker is paid for
// it; the text layer stands in for it, as for a page that did not render.
func TestVisionRefusesHugeRenderedPageBeforeChecker(t *testing.T) {
	for _, fullPage := range []bool{false, true} {
		model := &fakeModel{texts: []string{"Страница."}}
		checker := &fakeChecker{text: "Страница."}
		ocr := &VisionOCR{Model: model, Checker: checker, FullPage: fullPage, Render: func(context.Context, []byte, int) ([]byte, error) {
			return bombPNG(overLimit, overLimit), nil
		}}
		_, err := ocr.Recognize(context.Background(), OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Pages: 1})
		if !errors.Is(err, ErrImageTooLarge) {
			t.Errorf("full page %v: error = %v, want ErrImageTooLarge", fullPage, err)
		}
		if len(model.sizes) != 0 || len(checker.reqs) != 0 {
			t.Errorf("full page %v: model saw %v, checker %d requests; want neither called", fullPage, model.sizes, len(checker.reqs))
		}
	}

	ocr := &VisionOCR{Model: &fakeModel{}, FullPage: true, Render: func(context.Context, []byte, int) ([]byte, error) {
		return bombPNG(overLimit, overLimit), nil
	}}
	pages, err := ocr.Recognize(context.Background(), OCRRequest{Data: pdfBytes, MimeType: "application/pdf", Pages: 1, TextLayers: []string{page(1)}})
	if err != nil || len(pages) != 1 || pages[0].Text != page(1) || !pages[0].ReadFailed {
		t.Errorf("with a text layer: got %+v, %v; want the text layer, marked unread", pages, err)
	}
}

// pdfPage is a one-page PDF of the given size in points, blank.
func pdfPage(w, h int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] >>", w, h),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// A page as large as a poster is rendered at a lower resolution, so that
// pdftoppm does not hold a gigabyte bitmap; an A4 page keeps renderDPI.
func TestRenderPDFPageFitsPixelLimit(t *testing.T) {
	for _, tool := range []string{"pdftoppm", "pdfinfo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	a4, err := renderPDFPage(context.Background(), pdfPage(595, 842), 0)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(a4)); err != nil || cfg.Width != 1240 || cfg.Height != 1755 {
		t.Errorf("A4: %+v, %v; want 1240×1755 at %d dpi", cfg, err, renderDPI)
	}
	poster, err := renderPDFPage(context.Background(), pdfPage(7000, 7000), 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(poster))
	if err != nil || cfg.Width*cfg.Height > MaxOCRPixels || cfg.Width < 3000 {
		t.Errorf("poster: %+v, %v; want just within %d pixels", cfg, err, MaxOCRPixels)
	}
}

func TestPDFPageDPI(t *testing.T) {
	cases := []struct {
		info string
		want int
	}{
		{"Pages:           1\nPage    1 size:  595.28 x 841.89 pts (A4)\nPage    1 rot:   0\n", renderDPI},
		{"Page    3 size: 7000 x 7000 pts\n", 45},
		{"Page    1 size: 14400 x 200 pts\n", 150},
		{"no size here\n", renderDPI},
	}
	for _, c := range cases {
		if got := pageDPI(c.info); got != c.want {
			t.Errorf("pageDPI(%q) = %d, want %d", c.info, got, c.want)
		}
	}
}
