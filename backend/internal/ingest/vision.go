package ingest

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// VisionOCR reads pages with a language model that sees images, GigaChat in
// practice. On handwriting and formulas it reads far more than a classic OCR,
// but where it cannot make a word out it writes a plausible one, and on a
// sideways photo it makes up a whole page.
//
// So a Checker, a classic OCR such as Yandex, reads the same page first: it
// tells how the page is rotated, and lines of the model's reading whose words
// it did not read are dropped. Formulas and single words give it nothing to
// judge by and pass unchecked. Without a Checker the model's text is taken as
// is, and a photo is turned upright only by its EXIF orientation.
type VisionOCR struct {
	Model   providers.Provider
	Checker OCR // optional
	// Render turns page (0-based) of a PDF into a JPEG; nil: pdftoppm.
	Render func(ctx context.Context, pdf []byte, page int) ([]byte, error)
}

var _ OCR = (*VisionOCR)(nil)

const visionSystem = `Ты переписываешь в текст фотографию страницы рукописного конспекта.
Перепиши всё, что написано на основной странице, дословно и в порядке чтения, сохраняя строки и нумерацию пунктов.
Ничего не исправляй, не дополняй и не пересказывай: если слово написано с ошибкой или сокращено, оставь как есть.
Формулы записывай обычным текстом с символами Unicode: √, °, ∠, ⊥, ∥, ∩, ∈, α, β, ◡ (дуга), дроби через /. Например: sin 30° = 1/2, cos(180° − α) = −cos α, ∠AKB = ◡AK/2.
Рисунки и чертежи не описывай, подписи на них пропускай. Обрывки соседних страниц и надписи на закладках пропускай.
Неразборчивое место отметь как [неразборчиво]. Не пиши ничего, кроме текста страницы.`

const (
	visionHalfNote = "\nНа картинке верхняя или нижняя часть страницы: строки, обрезанные краем картинки, пропускай."
	visionUser     = "Перепиши текст этой страницы конспекта."
)

const (
	// splitHeight: a page image this tall or taller is read in two halves.
	// The model sees an image scaled down, and small handwriting in a whole
	// page comes out made up.
	splitHeight = 1000
	// halfShare of the height each half takes: they overlap, so that no line
	// is cut in both.
	halfShare = 0.55
	// seamLines of the top half are looked at for lines the bottom half
	// repeats.
	seamLines = 6
	// minJudgedWords: a line with fewer words of three letters or more gives
	// the checker nothing to judge by.
	minJudgedWords = 2
	// minLineSupport: share of a line's words the checker must have read.
	minLineSupport = 0.6
	// wordSimilarity: a word counts as read when one of the checker's words
	// is this close: "касательной" and "насательной".
	wordSimilarity = 0.75
	// renderDPI for scanned PDF pages: A4 at 150 dpi is 1240×1754.
	renderDPI = 150
)

// Recognize reads an image, or the pages of a PDF req.Only names (all when
// nil); pages not read come back empty.
func (v *VisionOCR) Recognize(ctx context.Context, req OCRRequest) ([]OCRPage, error) {
	if req.MimeType != "application/pdf" {
		p, err := v.readPage(ctx, req.Data, req.MimeType, req.Model)
		if err != nil {
			return nil, err
		}
		return []OCRPage{p}, nil
	}
	only := req.Only
	if only == nil {
		for n := range req.Pages {
			only = append(only, n)
		}
	}
	render := v.Render
	if render == nil {
		render = renderPDFPage
	}
	out := make([]OCRPage, req.Pages)
	for _, n := range only {
		if n < 0 || n >= req.Pages {
			continue
		}
		img, err := render(ctx, req.Data, n)
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", n+1, err)
		}
		if out[n], err = v.readPage(ctx, img, "image/jpeg", req.Model); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readPage has the checker read the page, then the model, and keeps the
// lines of the model's reading the checker backs. If the model fails, the
// checker's reading is better than nothing.
func (v *VisionOCR) readPage(ctx context.Context, data []byte, mime, model string) (OCRPage, error) {
	var check OCRPage
	checked := false
	if v.Checker != nil {
		got, err := v.Checker.Recognize(ctx, OCRRequest{Data: data, MimeType: mime, Model: model, Pages: 1})
		switch {
		case err != nil && ctx.Err() != nil:
			return OCRPage{}, err
		case err != nil:
			slog.Warn("ingest: checker OCR failed, reading unchecked", "err", err)
		case len(got) > 0:
			check, checked = got[0], true
		}
	}

	text, err := v.read(ctx, data, check.Rotate)
	switch {
	case err != nil && ctx.Err() != nil:
		return OCRPage{}, err
	case err != nil && checked:
		slog.Warn("ingest: vision model failed, taking the checker's reading", "err", err)
		return check, nil
	case err != nil:
		return OCRPage{}, err
	case !checked:
		return OCRPage{Text: text}, nil
	}

	kept, confirmed, judged := keepConfirmed(text, checkWords(check.Text))
	p := OCRPage{Text: kept, Rotate: check.Rotate}
	if judged > 0 {
		p.Confidence, p.HasConfidence = float64(confirmed)/float64(judged), true
	}
	if dropped := judged - confirmed; dropped > 0 {
		slog.Info("ingest: dropped lines the checker did not back", "dropped", dropped, "judged", judged)
	}
	return p, nil
}

// read turns the image upright, cuts a tall one in halves and has the model
// write out each.
func (v *VisionOCR) read(ctx context.Context, data []byte, rotate int) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	orientation := exifOrientation(data)
	if rotate != 0 {
		orientation = map[int]int{90: 6, 180: 3, 270: 8}[rotate]
	}
	page := orient(toGray(img), orientation)

	parts := []*image.Gray{page}
	if w, h := page.Bounds().Dx(), page.Bounds().Dy(); h >= splitHeight {
		half := int(float64(h) * halfShare)
		parts = []*image.Gray{
			page.SubImage(image.Rect(0, 0, w, half)).(*image.Gray),
			page.SubImage(image.Rect(0, h-half, w, h)).(*image.Gray),
		}
	}

	system := visionSystem
	if len(parts) > 1 {
		system += visionHalfNote
	}
	var texts []string
	for _, part := range parts {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, part, &jpeg.Options{Quality: 90}); err != nil {
			return "", fmt.Errorf("encode image: %w", err)
		}
		resp, err := v.Model.Complete(ctx, providers.Request{
			System: system,
			User:   visionUser,
			Image:  &providers.Image{Data: b.Bytes(), MimeType: "image/jpeg"},
		})
		if err != nil {
			return "", err
		}
		texts = append(texts, strings.TrimSpace(string(resp.Content)))
	}
	return joinHalves(texts), nil
}

// joinHalves drops the lines the bottom half repeats from the overlap.
func joinHalves(texts []string) string {
	if len(texts) < 2 {
		return strings.Join(texts, "\n")
	}
	top := strings.Split(texts[0], "\n")
	bottom := strings.Split(texts[1], "\n")
	seam := map[string]bool{}
	for _, l := range top[max(0, len(top)-seamLines):] {
		seam[lineKey(l)] = true
	}
	for len(bottom) > 0 && (seam[lineKey(bottom[0])] || strings.TrimSpace(bottom[0]) == "") {
		bottom = bottom[1:]
	}
	return strings.TrimSpace(strings.Join(append(top, bottom...), "\n"))
}

func lineKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// checkerWords are the words a classic OCR read, for lineConfirmed.
type checkerWords struct {
	set  map[string]bool
	list []string
}

// reHyphenLine joins a word broken over lines: "измеряет-\nся".
var reHyphenLine = regexp.MustCompile(`(\pL)-\s*\n\s*(\pL)`)

func checkWords(text string) checkerWords {
	w := checkerWords{set: map[string]bool{}}
	for _, word := range lineWords(reHyphenLine.ReplaceAllString(text, "$1$2")) {
		if !w.set[word] {
			w.set[word] = true
			w.list = append(w.list, word)
		}
	}
	return w
}

// lineConfirmed reports whether the checker read most words of a line of
// the model's reading. judged is false for a line with too few words.
func lineConfirmed(line string, w checkerWords) (confirmed, judged bool) {
	words := lineWords(line)
	if len(words) < minJudgedWords {
		return false, false
	}
	read := 0
	for _, word := range words {
		if w.set[word] || closeWord(word, w.list) {
			read++
		}
	}
	return float64(read) >= minLineSupport*float64(len(words)), true
}

// keepConfirmed drops the judged lines the checker does not back.
func keepConfirmed(text string, w checkerWords) (kept string, confirmed, judged int) {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		ok, j := lineConfirmed(line, w)
		if j {
			judged++
			if !ok {
				continue
			}
			confirmed++
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n")), confirmed, judged
}

// lineWords are the words of three letters or more, lowercased, ё as е.
func lineWords(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if len([]rune(f)) >= 3 {
			out = append(out, strings.ReplaceAll(f, "ё", "е"))
		}
	}
	return out
}

func closeWord(word string, list []string) bool {
	a := []rune(word)
	for _, c := range list {
		b := []rune(c)
		if longer := max(len(a), len(b)); float64(abs(len(a)-len(b))) > (1-wordSimilarity)*float64(longer) {
			continue
		}
		if 1-float64(levenshtein(a, b))/float64(max(len(a), len(b))) >= wordSimilarity {
			return true
		}
	}
	return false
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// renderPDFPage renders one page of a PDF to a grayscale JPEG with pdftoppm,
// from poppler-utils like pdftotext.
func renderPDFPage(ctx context.Context, pdf []byte, page int) ([]byte, error) {
	dir, err := os.MkdirTemp("", "pomnibot-render-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "page")
	n := strconv.Itoa(page + 1)
	cmd := exec.CommandContext(ctx, "pdftoppm", "-f", n, "-l", n, "-singlefile",
		"-r", strconv.Itoa(renderDPI), "-gray", "-jpeg", in, out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return os.ReadFile(out + ".jpg")
}
