// Command cardsgen generates cards from files using a live model.
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
		file         = flag.String("file", "", "file with the notes; more files can follow as arguments")
		title        = flag.String("title", "", "document title for sourceRef (default: first file name)")
		provider     = flag.String("provider", defaultProvider(), "who writes the cards: openrouter, gigachat, yandexgpt or qwen")
		model        = flag.String("model", "", "override the selected provider's model")
		limit        = flag.Int("limit", 0, "max facts per document, 0 for no limit")
		concurrency  = flag.Int("concurrency", 0, "parallel page reading calls; generation uses one request")
		timeout      = flag.Duration("call-timeout", generator.DefaultCallTimeout, "timeout per model call")
		ocrMode      = flag.String("ocr", "vision", "how photos and scans are read: vision (GigaChat, checked by Yandex OCR) or yandex (Yandex OCR alone)")
		visionBy     = flag.String("vision-provider", "", "who reads pages: openrouter, gigachat or qwen; defaults to openrouter with -provider openrouter, otherwise gigachat")
		ocrModel     = flag.String("ocr-model", os.Getenv("YC_OCR_MODEL"), "Yandex OCR model for every page (handwritten, page...); default: handwritten for photos, page for scans")
		extractOnly  = flag.Bool("extract", false, "only extract the text of the notes and print it, no cards")
		asJSON       = flag.Bool("json", false, "print the result as JSON to stdout")
		budgetUSD    = flag.Float64("budget-usd", .15, "document USD cap with conservative in-flight reservations; unknown-price fallback is blocked")
		maxCalls     = flag.Int("max-calls", 96, "total provider attempts for reading and generation")
		readingCache = flag.String("reading-cache", "", "private persistent reading snapshot; independent of generator version")
		verbose      = flag.Bool("v", false, "debug logs")
	)
	flag.Parse()
	explicitBudget := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "budget-usd" {
			explicitBudget = true
		}
	})
	limits, limitErr := processingLimits(*provider, explicitBudget, *budgetUSD, *maxCalls)
	if limitErr != nil {
		return limitErr
	}
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

	if *ocrMode != "vision" && *ocrMode != "yandex" && *ocrMode != "local" {
		return fmt.Errorf("-ocr %q: want vision, yandex or local", *ocrMode)
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
	profile := modelProfile(*provider)
	readerProfile := modelProfile(*visionBy)
	if *concurrency <= 0 {
		*concurrency = readerProfile.Tasks
	}
	// CLI reading and writing are sequential phases; task limits remain separate.
	if writer != nil {
		writer = limitModel(writer, *provider)
	}
	if vision != nil {
		vision = limitModel(vision, *visionBy)
	}
	ledger := providers.NewBudget(limits)
	ctx = providers.WithBudget(ctx, ledger)
	started := time.Now()
	var notes ingest.Result
	readingHit := false
	cacheKey := ""
	if *readingCache != "" {
		readerIdentity := []any{*ocrMode, *ocrModel, "native-pdf-v1", os.Getenv("YC_OCR_BASE_URL"), os.Getenv("YC_FOLDER_ID"), os.Getenv("YC_API_KEY") != ""}
		if *ocrMode == "vision" {
			readerIdentity = append(readerIdentity, readerEnvironment(*visionBy))
		}
		// The router override also changes the actual reader if the client is shared.
		if *ocrMode == "vision" && *visionBy == *provider {
			readerIdentity = append(readerIdentity, *model)
		}
		var keyErr error
		cacheKey, keyErr = readingKey(paths, readerIdentity)
		if keyErr != nil {
			return keyErr
		}
		notes, readingHit = loadReading(*readingCache, cacheKey)
	}
	if !readingHit {
		var readErr error
		notes, readErr = extract(ctx, paths, *ocrModel, vision, *ocrMode == "local", *concurrency, *timeout)
		if readErr != nil {
			return readErr
		}
		if *readingCache != "" {
			if cacheErr := saveReading(*readingCache, cacheKey, notes); cacheErr != nil {
				return fmt.Errorf("save private reading: %w", cacheErr)
			}
		}
	} else {
		slog.Info("cardsgen: reused private reading")
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

	readingSeconds := time.Since(started).Seconds()
	began := time.Now()
	firstValidated := 0.0
	delivered := 0
	genOptions, err := generator.OptionsFromEnv()
	if err != nil {
		return err
	}
	gen := generator.NewGenerator(writer, generator.Options{
		ReasoningEffort: genOptions.ReasoningEffort, MaxTokens: genOptions.MaxTokens,
		SourceBudget:   profile.SourceBytes,
		MaxFactsPerDoc: *limit,
		CallTimeout:    *timeout,
		OnBatch: func(b generator.Batch) {
			if firstValidated == 0 {
				firstValidated = time.Since(started).Seconds()
			}
			count := generator.CardCount(b.Facts)
			delivered += count
			refs := map[string]bool{}
			for _, f := range b.Facts {
				refs[fmt.Sprint(f.Pages)] = true
			}
			fmt.Fprintf(os.Stderr, "[%7.1fs] +%d (всего %d) %s\n",
				time.Since(began).Seconds(), count, delivered, strings.Join(sortedKeys(refs), "; "))
		},
	})

	fmt.Fprintf(os.Stderr, "%s: %d символов, модель %s\n", strings.Join(paths, ", "), len([]rune(notes.Text)), modelName)
	doc := generator.Document{Text: notes.Text, Title: *title, PDFs: notes.PDFs}
	if len(notes.Document.Blocks) > 0 {
		doc.Structure = &notes.Document
	}
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
			ReadingSeconds float64               `json:"readingSeconds"`
			// FirstVerifiedSeconds is the legacy key for time to structural validation.
			FirstVerifiedSeconds float64                  `json:"firstVerifiedSeconds"`
			Budget               providers.BudgetSnapshot `json:"budget"`
			ReadingCacheHit      bool                     `json:"readingCacheHit"`
		}{res, notes, usage.snapshot(), time.Since(started).Seconds(), readingSeconds, firstValidated, ledger.Snapshot(), readingHit}
		out.Ingest.Text = "" // the notes themselves are not the output
		for i := range out.Ingest.Pages {
			out.Ingest.Pages[i].Image = nil
		}
		for i := range out.Ingest.Document.Pages {
			out.Ingest.Document.Pages[i].Image = nil
		}
		if err := enc.Encode(out); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
	} else {
		fmt.Print(formatCards(res.Facts) + formatStats(res.Stats))
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
func extract(ctx context.Context, paths []string, ocrModel string, vision providers.Provider, local bool, concurrency int, callTimeout time.Duration) (ingest.Result, error) {
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
	var ocr ingest.OCR
	if !local && (!hasPDF || len(files) != 1) {
		cfg, err := yandex.ConfigFromEnv()
		var checker ingest.OCR
		if err == nil {
			client, createErr := yandex.New(cfg)
			if createErr != nil {
				return ingest.Result{}, createErr
			}
			checker = client
		}
		if vision != nil {
			ocr = &ingest.VisionOCR{Model: vision, Checker: checker, FullPage: true, Concurrency: concurrency, CallTimeout: callTimeout}
		} else {
			if err != nil {
				return ingest.Result{}, err
			}
			ocr = checker
		}
	}
	ext, err := ingest.NewExtractor(ocr, ingest.Options{DisablePDF: !hasPDF, OCRModel: ocrModel, NativePDF: true})
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
	if strings.TrimSpace(res.Text) == "" && len(res.PDFs) == 0 {
		return ingest.Result{}, errors.New("в файлах не нашлось текста")
	}
	return res, nil
}

// formatCards prints each fact and the individual sources of its cards.
func formatCards(facts []generator.Fact) string {
	var w strings.Builder
	for i, f := range facts {
		fmt.Fprintf(&w, "\n== Факт %d: %s — стр. %v\n", i+1, f.Name, f.Pages)
		for _, c := range f.Cards {
			base := c.Base()
			kind, answer := "", ""
			switch v := c.(type) {
			case generator.FlipCard:
				kind, answer = "flip", v.Answer
			case generator.BooleanCard:
				kind, answer = "boolean", fmt.Sprint(v.Answer)
			case generator.InputCard:
				kind, answer = "input", v.Answer
			case generator.ChoiceCard:
				kind = "choice"
				a, _ := json.Marshal(v.RightAnswers)
				answer = string(a)
				fmt.Fprintf(&w, "     Варианты: %v\n", append(append([]string{}, v.RightAnswers...), v.WrongAnswers...))
			case generator.TableCard:
				kind = "table"
				a, _ := json.Marshal(v.Columns)
				answer = string(a)
			}
			fmt.Fprintf(&w, "  [%s] %s\n     О: %s\n     Объяснение: %s\n     Цитата: «%s»\n", kind, base.Question, answer, base.Source, base.SourceQuote)
		}
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
	fmt.Fprintf(&w, "Вызовов модели:        %d (невалидных ответов %d) %s\n", s.ModelCalls, s.InvalidResponses, formatCounts(s.CallsByModel))
	fmt.Fprintf(&w, "Токены:                prompt %d, completion %d\n", s.PromptTokens, s.CompletionTokens)
	fmt.Fprintf(&w, "Стоимость по API:      $%.7f\n", s.CostUSD)
	fmt.Fprintf(&w, "Карточек от модели:    %d\n", s.CardsFromModel)
	fmt.Fprintf(&w, "Синтезировано flip/input: %d\n", s.CardsSynthesized)
	fmt.Fprintf(&w, "Цитата не найдена:     %d (%s)\n", s.DroppedQuote, pct(s.DroppedQuote))
	fmt.Fprintf(&w, "Неверная структура:    %d\n", s.DroppedInvalid)
	fmt.Fprintf(&w, "Точные повторы:        %d\n", s.DroppedDuplicate)
	fmt.Fprintf(&w, "Сверх лимита:          %d\n", s.DroppedByLimit)
	fmt.Fprintf(&w, "Итого карточек:        %d\n", s.Cards)
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
