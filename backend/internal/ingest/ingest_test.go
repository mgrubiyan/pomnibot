package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fakePDF stands in for pdftotext: it returns the pages it was given.
type fakePDF struct {
	pages []string
	err   error
	calls int
}

func (f *fakePDF) Pages(context.Context, []byte) ([]string, error) {
	f.calls++
	return f.pages, f.err
}

var pdfBytes = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<<>>\nendobj\n")

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// page is a page of typed notes, well over DefaultMinPageChars.
func page(n int) string {
	return fmt.Sprintf("Страница %d. %s", n, strings.Repeat("Митоз — непрямое деление соматических клеток. ", 5))
}

func newExtractor(t *testing.T, pdf PDFText) *Extractor {
	t.Helper()
	e, err := NewExtractor(nil, Options{PDF: pdf})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDetectKind(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want Kind
	}{
		{"PDF", pdfBytes, KindPDF},
		{"PNG", pngBytes(t, 4, 4), KindPNG},
		{"JPEG", jpegBytes(t, 4, 4), KindJPEG},
		{"WebP", webpBytes(t), KindWebP},
		{"UTF-8 text", []byte("Митоз — деление"), KindText},
		{"Windows-1251 text", []byte{0xCC, 0xE8, 0xF2, 0xEE, 0xE7}, KindText},
		{"binary", []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0x00}, KindUnsupported},
	}
	for _, tt := range tests {
		if got := DetectKind(tt.data); got != tt.want {
			t.Errorf("%s: DetectKind() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExtractGoesByContentNotName(t *testing.T) {
	pdf := &fakePDF{pages: []string{page(1)}}
	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{{Name: "photo.jpg", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if pdf.calls != 1 || res.Source != SourceText || !strings.Contains(res.Text, "Страница 1") {
		t.Errorf("a PDF named photo.jpg was not read as PDF: calls %d, %+v", pdf.calls, res)
	}
}

func TestExtractPDFTextLayer(t *testing.T) {
	pdf := &fakePDF{pages: []string{page(1), page(2) + "\n"}}
	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{{Name: "lecture.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if res.Source != SourceText || len(res.Pages) != 2 || len(res.Warnings) != 0 {
		t.Errorf("result = %+v", res)
	}
	if want := strings.TrimSpace(page(1)) + "\n" + strings.TrimSpace(page(2)); res.Text != want {
		t.Errorf("pages of one PDF must be joined by a single line break\n got: %q\nwant: %q", res.Text, want)
	}
	for i, p := range res.Pages {
		if p.Number != i+1 || p.Page != i+1 || p.NeedsOCR || p.Chars < DefaultMinPageChars {
			t.Errorf("page %d = %+v", i, p)
		}
	}
}

// LaTeX slides with Type 3 fonts carry no Unicode map, and pdftotext gives
// their Cyrillic as Latin-1: Windows-1251 codes, or T2A with "ё" at 0xBC.
func TestExtractRecodesCyrillicReadAsLatin1(t *testing.T) {
	// The heading comes from another font, with a proper Unicode map.
	mojibake := "Лекция 5. Ïðåïðîöåññîð — ïåðâàÿ ôàçà òðàíñëÿöèè. Äèðåêòèâû — å¼ èíñòðóêöèè, îíè íà÷èíàþòñÿ ñ ñèìâîëà '#': #include <iostream>. " +
		"Ïëþñû: ïðîñòîòà çàïóñêà, âîçìîæíîñòü áûñòðî òåñòèðîâàòü è èçìåíÿòü êîä áåç êîìïèëÿöèè. ¨ëêà è ¸æ."
	want := "Лекция 5. Препроцессор — первая фаза трансляции. Директивы — её инструкции, они начинаются с символа '#': #include <iostream>. " +
		"Плюсы: простота запуска, возможность быстро тестировать и изменять код без компиляции. Ёлка и ёж."
	// A slide of code: far more ASCII letters than recoded ones.
	code := "Ïðèìåð ïðîñòðàíñòâà èì¸í:\nnamespace math {\n  int square(int x) { return x * x; }\n}\nint main() {\n  std::cout << math::square(3) << std::endl;\n  using namespace std;\n  return 0;\n}"
	codeWant := "Пример пространства имён:\nnamespace math {\n  int square(int x) { return x * x; }\n}\nint main() {\n  std::cout << math::square(3) << std::endl;\n  using namespace std;\n  return 0;\n}"
	french := "Le café est très apprécié à Paris, où l'on préfère l'expresso. " + page(2)
	pdf := &fakePDF{pages: []string{mojibake, code, french}}

	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{{Name: "slides.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if wantText := want + "\n" + codeWant + "\n" + strings.TrimSpace(french); res.Text != wantText {
		t.Errorf("text:\n got: %q\nwant: %q", res.Text, wantText)
	}
	if len(res.Pages) != 3 || !res.Pages[0].Recoded || !res.Pages[1].Recoded || res.Pages[2].Recoded {
		t.Errorf("pages = %+v, want the first two recoded", res.Pages)
	}
}

// Cards cite pages, so the generator needs to know where each one starts in
// the joined text: pages of a PDF are joined by a line break, files by an
// empty line, and a blank page starts where the next one does.
func TestExtractRecordsWherePagesStart(t *testing.T) {
	pdf := &fakePDF{pages: []string{"  " + page(1), "", page(3)}}
	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{
		{Name: "lecture.pdf", Data: pdfBytes},
		{Name: "notes.txt", Data: []byte(page(4))},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(res.Pages) != 4 {
		t.Fatalf("got %d pages, want 4", len(res.Pages))
	}
	for i, want := range []int{1, 3, 3, 4} {
		start := res.Pages[i].Start
		if head := strings.TrimSpace(page(want))[:20]; !strings.HasPrefix(res.Text[start:], head) {
			t.Errorf("page %d starts at %d with %q, want %q", i+1, start, res.Text[start:min(start+20, len(res.Text))], head)
		}
	}
	if !strings.Contains(res.Text, strings.TrimSpace(page(3))+"\n\n"+strings.TrimSpace(page(4))) {
		t.Errorf("files must be joined by an empty line: %q", res.Text)
	}
}

func TestExtractMarksScannedPages(t *testing.T) {
	pdf := &fakePDF{pages: []string{page(1), "  12 \n", page(3)}}
	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{{Name: "scan.pdf", Data: pdfBytes}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !res.Pages[1].NeedsOCR || res.Pages[0].NeedsOCR || res.Pages[2].NeedsOCR {
		t.Errorf("pages = %+v, want only page 2 marked for OCR", res.Pages)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "Страница 2") {
		t.Errorf("warnings = %q", res.Warnings)
	}
}

func TestExtractSeveralFilesInOrder(t *testing.T) {
	pdf := &fakePDF{pages: []string{page(2), page(3)}}
	res, err := newExtractor(t, pdf).Extract(context.Background(), []File{
		{Name: "1.txt", Data: []byte(page(1))},
		{Name: "2.pdf", Data: pdfBytes},
		{Name: "3.txt", Data: []byte(page(4))},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	var order []int
	for n := 1; n <= 4; n++ {
		order = append(order, strings.Index(res.Text, fmt.Sprintf("Страница %d.", n)))
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] < 0 || order[i] <= order[i-1] {
			t.Fatalf("pages out of order: positions %v", order)
		}
	}
	var got []string
	for _, p := range res.Pages {
		got = append(got, fmt.Sprintf("%d:%d/%d", p.Number, p.File, p.Page))
	}
	if want := "1:0/1 2:1/1 3:1/2 4:2/1"; strings.Join(got, " ") != want {
		t.Errorf("pages = %v, want %s", got, want)
	}
}

func TestExtractTextEncodings(t *testing.T) {
	e := newExtractor(t, &fakePDF{})
	tests := []struct {
		name    string
		data    []byte
		warning bool
	}{
		{"UTF-8", []byte("Митоз"), false},
		{"UTF-8 with BOM", append([]byte{0xEF, 0xBB, 0xBF}, "Митоз"...), false},
		{"Windows-1251", []byte{0xCC, 0xE8, 0xF2, 0xEE, 0xE7}, true},
		{"UTF-16LE", []byte{0xFF, 0xFE, 0x1C, 0x04, 0x38, 0x04, 0x42, 0x04, 0x3E, 0x04, 0x37, 0x04}, true},
	}
	for _, tt := range tests {
		res, err := e.Extract(context.Background(), []File{{Name: "notes.txt", Data: tt.data}})
		if err != nil {
			t.Fatalf("%s: Extract() error = %v", tt.name, err)
		}
		if res.Text != "Митоз" {
			t.Errorf("%s: text = %q, want Митоз", tt.name, res.Text)
		}
		if (len(res.Warnings) > 0) != tt.warning {
			t.Errorf("%s: warnings = %q", tt.name, res.Warnings)
		}
	}
}

func TestExtractRejectsUnsupported(t *testing.T) {
	e := newExtractor(t, &fakePDF{})
	for name, want := range map[string]struct {
		data []byte
		err  error
	}{
		"binary":            {[]byte{0x00, 0x01, 0x02, 0xFF}, ErrUnsupported},
		"photo without OCR": {pngBytes(t, 4, 4), ErrNoOCR},
	} {
		_, err := e.Extract(context.Background(), []File{{Name: name, Data: want.data}})
		var ie *Error
		if !errors.As(err, &ie) || !errors.Is(err, want.err) || ie.Message == "" || ie.File != name {
			t.Errorf("%s: error = %v, want *Error with %v and a message", name, err, want.err)
		}
	}

	// The log tells what came instead: a page, a HEIC photo...
	_, err := e.Extract(context.Background(), []File{{Name: "photo_1.jpg", Data: []byte("<!DOCTYPE html><html><body>Нет доступа</body></html>")}})
	if err == nil || !strings.Contains(err.Error(), "text/html") {
		t.Errorf("error = %v, want the detected type in it", err)
	}
}

// webpBytes is a 64×48 photo in WebP, the format a messenger may store a
// photo in whatever its file name says.
func webpBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/photo.webp")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExtractReportsBrokenPDF(t *testing.T) {
	pdf := &fakePDF{err: errors.New("exit status 1: Syntax Error")}
	_, err := newExtractor(t, pdf).Extract(context.Background(), []File{{Name: "broken.pdf", Data: pdfBytes}})
	var ie *Error
	if !errors.As(err, &ie) || !strings.Contains(ie.Message, "PDF") {
		t.Errorf("error = %v, want *Error about the PDF", err)
	}
}

func TestNewChecksPDFToText(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := NewExtractor(nil, Options{}); !errors.Is(err, ErrNoPDFToText) {
		t.Errorf("NewExtractor() without pdftotext: err = %v, want ErrNoPDFToText", err)
	}

	e, err := NewExtractor(nil, Options{DisablePDF: true})
	if err != nil {
		t.Fatalf("NewExtractor(DisablePDF) error = %v", err)
	}
	if _, err := e.Extract(context.Background(), []File{{Name: "a.txt", Data: []byte("текст")}}); err != nil {
		t.Errorf("text without pdftotext: %v", err)
	}
	if _, err := e.Extract(context.Background(), []File{{Name: "a.pdf", Data: pdfBytes}}); !errors.Is(err, ErrNoPDF) {
		t.Errorf("PDF with PDFs disabled: err = %v, want ErrNoPDF", err)
	}
}

func TestSplitPages(t *testing.T) {
	for in, want := range map[string]int{
		"первая\fвторая\f":   2,
		"первая\f\fтретья\f": 3, // an empty page in the middle stays
		"":                   0,
		"без разделителя":    1,
	} {
		if got := splitPages(in); len(got) != want {
			t.Errorf("splitPages(%q) = %d pages, want %d", in, len(got), want)
		}
	}
}

// minimalPDF builds a one-page PDF with a text layer, for the real pdftotext.
func minimalPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFToTextBinary(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	p, err := newPDFToText("pdftotext")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := p.Pages(context.Background(), minimalPDF("Mitosis is indirect cell division"))
	if err != nil {
		t.Fatalf("Pages() error = %v", err)
	}
	if len(pages) != 1 || !strings.Contains(pages[0], "Mitosis is indirect cell division") {
		t.Errorf("pages = %q", pages)
	}
}

func TestRecodeLatin1(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"short words do not count against", "Â äàííîì êîäå pi çàìåíèòñÿ íà 4, à âñå âõîæäåíèÿ r - íà 2.",
			"В данном коде pi заменится на 4, а все вхождения r - на 2."},
		{"a two-word heading over code", "Çàïóñê ïðåïðîöåññîðà\ng++ -E main.cpp -o main.i\ncat main.i | head -n 20",
			"Запуск препроцессора\ng++ -E main.cpp -o main.i\ncat main.i | head -n 20"},
		{"German stays", "Die Größe der Äpfel hängt vom Boden ab, sagt Müller.", ""},
		{"a French name stays", "Как писал Жан-Поль Сартр в книге «L'Être et le néant», свобода — это выбор.", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := recodeLatin1(tt.in)
			if tt.want == "" {
				if ok || got != tt.in {
					t.Errorf("recoded %q as %q", tt.in, got)
				}
				return
			}
			if !ok || got != tt.want {
				t.Errorf("recodeLatin1() = %q, %v; want %q", got, ok, tt.want)
			}
		})
	}
}
