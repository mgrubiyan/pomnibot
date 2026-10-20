// Command cardsgen runs card generation on notes against a live model and
// prints the cards, the stats and the timings. It is for measuring quality
// and latency on real notes before there is storage.
//
// It takes text files, PDFs and photos of pages, told apart by content.
// Several files are pages of one set of notes, in the order given. PDFs need
// pdftotext and pdftoppm (poppler-utils). Photos and scanned pages are read
// by GigaChat and checked by Yandex Vision OCR (YC_API_KEY, and YC_FOLDER_ID
// unless the key is a service account's), which also turns them upright;
// -ocr yandex reads them with Yandex alone.
//
// -provider yandexgpt or qwen writes the cards with a Yandex AI Studio model
// instead, to compare it with GigaChat on the same notes (YC_API_KEY,
// YC_FOLDER_ID, YC_YANDEXGPT_MODEL for YandexGPT, YC_LLM_MODEL for the app's
// model, Qwen by default). -vision-provider qwen reads the photos with the
// app's model instead of GigaChat, still checked by Yandex OCR.
//
//	task cardsgen -- notes.txt
//	task cardsgen -- -model GigaChat-2-Max -chunk 2000 -limit 30 -json lecture.pdf page2.pdf > run.json
//	task cardsgen -- -provider yandexgpt -ocr yandex lecture.pdf
//	task cardsgen -- -extract -vision-provider qwen page1.jpg page2.jpg
//
// -provider openrouter uses OPENROUTER_KEY and defaults to Haiku 5.5 with
// thinking disabled. It also reads full PDF pages through OpenRouter.
//
// Credentials come from OPENROUTER_*, GIGACHAT_* and YC_* variables: task passes the ones
// from .env in the repository root.
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
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/yandexgpt"
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
		provider    = flag.String("provider", defaultProvider(), "who writes the cards: openrouter, gigachat, yandexgpt or qwen")
		model       = flag.String("model", "", "override the selected provider's model")
		chunk       = flag.Int("chunk", generator.DefaultChunkSize, "fragment size in characters")
		overlap     = flag.Int("overlap", generator.DefaultChunkOverlap, "fragment overlap in characters, -1 for none")
		limit       = flag.Int("limit", 0, "max facts per document, 0 for no limit")
		perChunk    = flag.Int("per-chunk", generator.DefaultMaxFactsPerChunk, "max facts per fragment, each with up to 4 cards")
		concurrency = flag.Int("concurrency", generator.DefaultConcurrency, "parallel model calls; freemium allows 1")
		timeout     = flag.Duration("call-timeout", generator.DefaultCallTimeout, "timeout per model call")
		ocrMode     = flag.String("ocr", "vision", "how photos and scans are read: vision (GigaChat, checked by Yandex OCR) or yandex (Yandex OCR alone)")
		visionBy    = flag.String("vision-provider", "", "who reads pages: openrouter, gigachat or qwen; defaults to openrouter with -provider openrouter, otherwise gigachat")
		ocrModel    = flag.String("ocr-model", os.Getenv("YC_OCR_MODEL"), "Yandex OCR model for every page (handwritten, page...); default: handwritten for photos, page for scans")
		extractOnly = flag.Bool("extract", false, "only extract the text of the notes and print it, no cards")
		asJSON      = flag.Bool("json", false, "print the result as JSON to stdout")
		verbose     = flag.Bool("v", false, "debug logs")
	)
	flag.Parse()
	if *visionBy == "" {
		*visionBy = "gigachat"
		if *provider == "openrouter" {
			*visionBy = "openrouter"
		}
	}
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

	if *ocrMode != "vision" && *ocrMode != "yandex" {
		return fmt.Errorf("-ocr %q: want vision or yandex", *ocrMode)
	}
	if *provider != "gigachat" && *provider != "yandexgpt" && *provider != "qwen" && *provider != "openrouter" {
		return fmt.Errorf("-provider %q: want openrouter, gigachat, yandexgpt or qwen", *provider)
	}
	if *visionBy != "gigachat" && *visionBy != "qwen" && *visionBy != "openrouter" {
		return fmt.Errorf("-vision-provider %q: want openrouter, gigachat or qwen", *visionBy)
	}
	// GigaChat writes the cards, unless -provider yandexgpt, and reads the
	// photos, unless -ocr yandex.
	var client *gigachat.Client
	modelName := "GigaChat-3-Ultra"
	if (!*extractOnly && *provider == "gigachat") || (*ocrMode == "vision" && *visionBy == "gigachat") {
		cfg, err := gigachat.ConfigFromEnv()
		if err != nil {
			return err
		}
		if *model != "" && *provider == "gigachat" {
			cfg.Model = *model
		}
		modelName = or(cfg.Model, modelName)
		if client, err = gigachat.New(cfg); err != nil {
			return err
		}
		defer client.Close()
	}
	var writer providers.Provider = client
	if !*extractOnly && *provider == "yandexgpt" {
		cfg, err := yandexgpt.ConfigFromEnv()
		if err != nil {
			return err
		}
		if *model != "" {
			cfg.Model = *model
		}
		modelName = cfg.Model
		if writer, err = yandexgpt.New(cfg); err != nil {
			return err
		}
	}
	if !*extractOnly && *provider == "qwen" {
		cfg, err := aistudio.ConfigFromEnv()
		if err != nil {
			return err
		}
		if *model != "" {
			cfg.Model = *model
		}
		modelName = cfg.Model
		if writer, err = aistudio.New(cfg); err != nil {
			return err
		}
	}
	var router *openrouter.Client
	if (!*extractOnly && *provider == "openrouter") || (*ocrMode == "vision" && *visionBy == "openrouter") {
		cfg, err := openrouter.ConfigFromEnv()
		if err != nil {
			return err
		}
		if *model != "" && *provider == "openrouter" {
			cfg.Model = *model
		}
		router, err = openrouter.New(cfg)
		if err != nil {
			return err
		}
		if *provider == "openrouter" {
			writer, modelName = router, cfg.Model
		}
	}

	var vision providers.Provider
	switch {
	case *ocrMode == "vision" && *visionBy == "openrouter":
		vision = router
	case *ocrMode == "vision" && *visionBy == "qwen":
		cfg, err := aistudio.ConfigFromEnv()
		if err != nil {
			return err
		}
		if vision, err = aistudio.New(cfg); err != nil {
			return err
		}
	case *ocrMode == "vision":
		vision = client
	}
	usage := &runUsage{}
	if writer != nil {
		writer = trackedProvider{Provider: writer, usage: usage}
	}
	if vision != nil {
		vision = trackedProvider{Provider: vision, usage: usage}
	}
	started := time.Now()
	notes, err := extract(ctx, paths, *ocrModel, vision, *ocrMode == "vision" && *visionBy == "openrouter")
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

	began := time.Now()
	delivered := 0
	gen := generator.NewGenerator(writer, generator.Options{
		Concurrency:      *concurrency,
		ChunkSize:        *chunk,
		ChunkOverlap:     *overlap,
		MaxFactsPerChunk: *perChunk,
		MaxFactsPerDoc:   *limit,
		CallTimeout:      *timeout,
		OnBatch: func(b generator.Batch) {
			delivered += len(b.Cards)
			refs := map[string]bool{}
			for _, c := range b.Cards {
				refs[c.SourceRef] = true
			}
			fmt.Fprintf(os.Stderr, "[%7.1fs] +%d (всего %d) %s\n",
				time.Since(began).Seconds(), len(b.Cards), delivered, strings.Join(sortedKeys(refs), "; "))
		},
	})

	fmt.Fprintf(os.Stderr, "%s: %d символов, модель %s\n", strings.Join(paths, ", "), len([]rune(notes.Text)), modelName)
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
			Ingest         ingest.Result         `json:"ingest"`
			ModelUsage     map[string]modelUsage `json:"modelUsage"`
			ElapsedSeconds float64               `json:"elapsedSeconds"`
		}{res, notes, usage.snapshot(), time.Since(started).Seconds()}
		out.Ingest.Text = "" // the notes themselves are not the output
		if err := enc.Encode(out); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
	} else {
		fmt.Print(formatCards(res.Facts, res.Cards) + formatStats(res.Stats))
	}
	totals := usage.snapshot()
	for _, name := range sortedKeys(totals) {
		u := totals[name]
		fmt.Fprintf(os.Stderr, "%s: %d вызовов, prompt %d, completion %d, стоимость по API $%.6f\n", name, u.Calls, u.PromptTokens, u.CompletionTokens, u.CostUSD)
	}
	fmt.Fprintf(os.Stderr, "Включая чтение: %.1fs\n", time.Since(started).Seconds())
	if genErr != nil {
		return fmt.Errorf("generation stopped: %w", genErr)
	}
	return nil
}

// extract reads the files and pulls the text out of them. pdftotext is
// required only when one of them is a PDF.
func extract(ctx context.Context, paths []string, ocrModel string, vision providers.Provider, fullPages bool) (ingest.Result, error) {
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

	// Yandex OCR turns photos upright and checks GigaChat's reading, or
	// reads them alone with -ocr yandex.
	cfg, err := yandex.ConfigFromEnv()
	if err != nil {
		return ingest.Result{}, err
	}
	client, err := yandex.New(cfg)
	if err != nil {
		return ingest.Result{}, err
	}
	var ocr ingest.OCR = client
	if vision != nil {
		ocr = &ingest.VisionOCR{Model: vision, Checker: client, FullPage: fullPages}
	}
	ext, err := ingest.NewExtractor(ocr, ingest.Options{DisablePDF: !hasPDF, OCRModel: ocrModel, ReadAllPDFPages: fullPages})
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
func formatCards(facts []cards.Fact, list []cards.Card) string {
	byID := make(map[string]cards.Fact, len(facts))
	for _, f := range facts {
		byID[f.ID] = f
	}
	var w strings.Builder
	fact, lastID := 0, ""
	for _, c := range list {
		if c.FactID != lastID {
			fact++
			lastID = c.FactID
			f := byID[c.FactID]
			fmt.Fprintf(&w, "\n== Факт %d: %s (%s) — %s\n", fact, f.Name, f.Topic, c.SourceRef)
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
		if c.Table != nil {
			for _, col := range c.Table.Columns {
				var terms []string
				for _, item := range c.Table.Items {
					if item.Column == col {
						terms = append(terms, item.Text)
					}
				}
				fmt.Fprintf(&w, "     %s: %s\n", col, strings.Join(terms, ", "))
			}
		} else {
			fmt.Fprintf(&w, "     О: %s\n", c.Answer)
		}
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
	fmt.Fprintf(&w, "  отказ цензора:       %d\n", s.ChunksRefused)
	fmt.Fprintf(&w, "Вызовов модели:        %d (невалидных ответов %d) %s\n", s.ModelCalls, s.InvalidResponses, formatCounts(s.CallsByModel))
	fmt.Fprintf(&w, "Токены:                prompt %d, completion %d\n", s.PromptTokens, s.CompletionTokens)
	fmt.Fprintf(&w, "Фактов:                %d от модели, %d в итоге\n", s.FactsFromModel, s.Facts)
	fmt.Fprintf(&w, "Карточек от модели:    %d\n", s.CardsFromModel)
	fmt.Fprintf(&w, "  цитата не найдена:   %d (%s)\n", s.DroppedQuote, pct(s.DroppedQuote))
	fmt.Fprintf(&w, "  ответ не из цитаты или есть в вопросе: %d (%s)\n", s.DroppedUnsupported, pct(s.DroppedUnsupported))
	fmt.Fprintf(&w, "  таблица не по цитате: %d (%s)\n", s.DroppedTable, pct(s.DroppedTable))
	fmt.Fprintf(&w, "  о деталях примера:   %d (%s)\n", s.DroppedExample, pct(s.DroppedExample))
	fmt.Fprintf(&w, "  дубликаты:           %d (%s)\n", s.DroppedDuplicate, pct(s.DroppedDuplicate))
	fmt.Fprintf(&w, "  не по схеме:         %d (%s)\n", s.DroppedInvalid, pct(s.DroppedInvalid))
	fmt.Fprintf(&w, "  сверх лимита:        %d (%s)\n", s.DroppedByLimit, pct(s.DroppedByLimit))
	fmt.Fprintf(&w, "  вариант не удержал вид: %d (%s; факт покрыт другими)\n", s.DroppedVariants, pct(s.DroppedVariants))
	fmt.Fprintf(&w, "  choice → flip:       %d (мало дистракторов)\n", s.DowngradedToFlip)
	fmt.Fprintf(&w, "Отброшено вариантов:   %d (есть в цитате или вопросе)\n", s.DroppedDistractors)
	fmt.Fprintf(&w, "  input → flip:        %d (длинный ответ)\n", s.InputToFlip)
	fmt.Fprintf(&w, "Объяснений урезано:   %d (о конспекте или не из него)\n", s.TrimmedExplanations)
	fmt.Fprintf(&w, "Личные данные:        %d карточек отброшено\n", s.DroppedPersonal)
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
