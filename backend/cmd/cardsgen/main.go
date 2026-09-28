// Command cardsgen runs card generation on notes against live GigaChat and
// prints the cards, the stats and the timings. It is for measuring quality
// and latency on real notes before there is storage.
//
// It takes text files, PDFs and photos of pages, told apart by content.
// Several files are pages of one set of notes, in the order given. PDFs need
// pdftotext (poppler-utils); photos and scanned pages need Yandex Vision OCR
// (YC_API_KEY, and YC_FOLDER_ID unless the key is a service account's).
//
//	task cardsgen -- notes.txt
//	task cardsgen -- -model GigaChat-2-Max -chunk 2000 -limit 30 -json lecture.pdf page2.pdf > run.json
//
// Credentials come from GIGACHAT_* variables: task passes the ones from .env
// in the repository root.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cardsgen:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		file        = flag.String("file", "", "file with the notes; more files can follow as arguments")
		title       = flag.String("title", "", "document title for sourceRef (default: first file name)")
		model       = flag.String("model", "", "model, overrides GIGACHAT_MODEL")
		chunk       = flag.Int("chunk", generator.DefaultChunkSize, "fragment size in characters")
		overlap     = flag.Int("overlap", generator.DefaultChunkOverlap, "fragment overlap in characters, -1 for none")
		limit       = flag.Int("limit", 0, "max facts per document, 0 for no limit")
		perChunk    = flag.Int("per-chunk", generator.DefaultMaxFactsPerChunk, "max facts per fragment, each with up to 4 cards")
		concurrency = flag.Int("concurrency", generator.DefaultConcurrency, "parallel model calls; freemium allows 1")
		timeout     = flag.Duration("call-timeout", generator.DefaultCallTimeout, "timeout per model call")
		ocrModel    = flag.String("ocr-model", os.Getenv("YC_OCR_MODEL"), "OCR model for every page (handwritten, page...); default: handwritten for photos, page for scans")
		extractOnly = flag.Bool("extract", false, "only extract the text of the notes and print it, no cards")
		asJSON      = flag.Bool("json", false, "print the result as JSON to stdout")
		verbose     = flag.Bool("v", false, "debug logs")
	)
	flag.Parse()
	paths := flag.Args()
	if *file != "" {
		paths = append([]string{*file}, paths...)
	}
	if len(paths) == 0 {
		flag.Usage()
		return errors.New("no input file")
	}

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	notes, err := extract(ctx, paths, *ocrModel)
	if err != nil {
		return err
	}
	if *title == "" {
		*title = strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0]))
	}
	if *extractOnly {
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			return enc.Encode(notes)
		}
		fmt.Println(notes.Text)
		return nil
	}

	cfg, err := gigachat.ConfigFromEnv()
	if err != nil {
		return err
	}
	if *model != "" {
		cfg.Model = *model
	}
	client, err := gigachat.New(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	began := time.Now()
	delivered := 0
	gen := generator.NewGenerator(client, generator.Options{
		Concurrency:      *concurrency,
		ChunkSize:        *chunk,
		ChunkOverlap:     *overlap,
		MaxFactsPerChunk: *perChunk,
		MaxFactsPerDoc:   *limit,
		CallTimeout:      *timeout,
		OnCards: func(batch []cards.Card) {
			delivered += len(batch)
			refs := map[string]bool{}
			for _, c := range batch {
				refs[c.SourceRef] = true
			}
			fmt.Fprintf(os.Stderr, "[%7.1fs] +%d (всего %d) %s\n",
				time.Since(began).Seconds(), len(batch), delivered, strings.Join(sortedKeys(refs), "; "))
		},
	})

	fmt.Fprintf(os.Stderr, "%s: %d символов, модель %s\n", strings.Join(paths, ", "), len([]rune(notes.Text)), or(cfg.Model, gigachat.DefaultModel))
	doc := generator.Document{Text: notes.Text, Title: *title}
	for _, p := range notes.Pages {
		doc.PageStarts = append(doc.PageStarts, p.Start)
	}
	res, genErr := gen.Generate(ctx, doc)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		out := struct {
			generator.Result
			Ingest ingest.Result `json:"ingest"`
		}{res, notes}
		out.Ingest.Text = "" // the notes themselves are not the output
		if err := enc.Encode(out); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
	} else {
		fmt.Print(formatCards(res.Cards) + formatStats(res.Stats))
	}
	if genErr != nil {
		return fmt.Errorf("generation stopped: %w", genErr)
	}
	return nil
}

// extract reads the files and pulls the text out of them. pdftotext is
// required only when one of them is a PDF.
func extract(ctx context.Context, paths []string, ocrModel string) (ingest.Result, error) {
	files := make([]ingest.File, 0, len(paths))
	hasPDF := false
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return ingest.Result{}, fmt.Errorf("read notes: %w", err)
		}
		files = append(files, ingest.File{Name: filepath.Base(path), Data: data})
		hasPDF = hasPDF || ingest.DetectKind(data) == ingest.KindPDF
	}

	// OCR only with credentials: without them text and typed PDFs still work.
	var ocr ingest.OCR
	if cfg := yandex.ConfigFromEnv(); cfg.APIKey != "" {
		client, err := yandex.New(cfg)
		if err != nil {
			return ingest.Result{}, err
		}
		ocr = client
	}
	ext, err := ingest.NewExtractor(ocr, ingest.Options{DisablePDF: !hasPDF, OCRModel: ocrModel})
	if err != nil {
		return ingest.Result{}, err
	}
	res, err := ext.Extract(ctx, files)
	var ie *ingest.Error
	if errors.As(err, &ie) {
		return ingest.Result{}, fmt.Errorf("%s: %s (%w)", ie.File, ie.Message, err)
	}
	if err != nil {
		return ingest.Result{}, err
	}

	fmt.Fprintf(os.Stderr, "Извлечено: %d стр., источник %s, %d символов\n",
		len(res.Pages), res.Source, len([]rune(res.Text)))
	for _, p := range res.Pages {
		mark := ""
		if p.Poor {
			mark = " (плохо)"
		}
		fmt.Fprintf(os.Stderr, "  стр. %d: %s, %d символов%s\n", p.Number, p.Source, p.Chars, mark)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "  ⚠", w)
	}
	if strings.TrimSpace(res.Text) == "" {
		return ingest.Result{}, errors.New("в файлах не нашлось текста")
	}
	return res, nil
}

// formatCards prints cards grouped by fact: the quote once, then every card
// that tests it.
func formatCards(list []cards.Card) string {
	var w strings.Builder
	fact, lastID := 0, ""
	for _, c := range list {
		if c.FactID != lastID {
			fact++
			lastID = c.FactID
			fmt.Fprintf(&w, "\n== Факт %d: %s (%s) — %s\n", fact, c.FactName, c.Topic, c.SourceRef)
			fmt.Fprintf(&w, "   Цитата: «%s»\n", c.SourceQuote)
		}
		fmt.Fprintf(&w, "  [%s] %s\n", c.Kind, c.Question)
		if len(c.Options) > 0 {
			opts := make([]string, len(c.Options))
			for j, o := range c.Options {
				opts[j] = o
				if o == c.Answer {
					opts[j] += " ✓"
				}
			}
			fmt.Fprintf(&w, "     Варианты: %s\n", strings.Join(opts, " | "))
		}
		fmt.Fprintf(&w, "     О: %s\n", c.Answer)
		fmt.Fprintf(&w, "     Объяснение: %s\n", c.Explanation)
	}
	return w.String()
}

func formatStats(s generator.Stats) string {
	var w strings.Builder
	pct := func(n int) string {
		if s.CardsFromModel == 0 {
			return "—"
		}
		return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(s.CardsFromModel))
	}
	sec := func(d time.Duration) string { return fmt.Sprintf("%.1fs", d.Seconds()) }

	fmt.Fprintln(&w, "\n== Статистика")
	fmt.Fprintf(&w, "Фрагментов:            %d\n", s.Chunks)
	fmt.Fprintf(&w, "  отсеяно фильтром:    %d %s\n", s.ChunksFiltered, formatCounts(s.FilteredBy))
	fmt.Fprintf(&w, "  пропущено по лимиту: %d\n", s.ChunksSkipped)
	fmt.Fprintf(&w, "  ошибка провайдера:   %d\n", s.ChunksFailed)
	fmt.Fprintf(&w, "  без валидного JSON:  %d\n", s.ChunksInvalid)
	fmt.Fprintf(&w, "Вызовов модели:        %d (невалидных ответов %d) %s\n", s.ModelCalls, s.InvalidResponses, formatCounts(s.CallsByModel))
	fmt.Fprintf(&w, "Токены:                prompt %d, completion %d\n", s.PromptTokens, s.CompletionTokens)
	fmt.Fprintf(&w, "Фактов:                %d от модели, %d в итоге\n", s.FactsFromModel, s.Facts)
	fmt.Fprintf(&w, "Карточек от модели:    %d\n", s.CardsFromModel)
	fmt.Fprintf(&w, "  цитата не найдена:   %d (%s)\n", s.DroppedQuote, pct(s.DroppedQuote))
	fmt.Fprintf(&w, "  ответ не из цитаты или есть в вопросе: %d (%s)\n", s.DroppedUnsupported, pct(s.DroppedUnsupported))
	fmt.Fprintf(&w, "  дубликаты:           %d (%s)\n", s.DroppedDuplicate, pct(s.DroppedDuplicate))
	fmt.Fprintf(&w, "  не по схеме:         %d (%s)\n", s.DroppedInvalid, pct(s.DroppedInvalid))
	fmt.Fprintf(&w, "  сверх лимита:        %d (%s)\n", s.DroppedByLimit, pct(s.DroppedByLimit))
	fmt.Fprintf(&w, "  вариант не удержал вид: %d (%s; факт покрыт другими)\n", s.DroppedVariants, pct(s.DroppedVariants))
	fmt.Fprintf(&w, "  choice → flip:       %d (мало дистракторов)\n", s.DowngradedToFlip)
	fmt.Fprintf(&w, "Отброшено вариантов:   %d (есть в цитате или вопросе)\n", s.DroppedDistractors)
	fmt.Fprintf(&w, "  input → flip:        %d (длинный ответ)\n", s.InputToFlip)
	fmt.Fprintf(&w, "Итого карточек:        %d\n", s.Cards)
	fmt.Fprintf(&w, "Время на фрагмент:     среднее %s, максимум %s\n", sec(s.ChunkTimeAvg), sec(s.ChunkTimeMax))
	fmt.Fprintf(&w, "Первая карточка через: %s\n", sec(s.FirstCardAfter))
	fmt.Fprintf(&w, "Всего:                 %s\n", sec(s.Total))
	return w.String()
}

func formatCounts(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
