// Command cardsgen runs card generation on a text file against live GigaChat
// and prints the cards, the stats and the timings. It is for measuring
// quality and latency on real notes before there is storage.
//
//	cd backend
//	go run ./cmd/cardsgen -file notes.txt
//	go run ./cmd/cardsgen -file notes.txt -model GigaChat-2-Max -chunk 2000 -limit 30 -json > run.json
//
// Credentials come from GIGACHAT_* variables; .env in the current or parent
// directory is read if present, without overriding variables already set.
package main

import (
	"bufio"
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

	"github.com/mgrubiyan/pomnibot/backend/internal/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/cards/gigachat"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cardsgen:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		file        = flag.String("file", "", "text file with the notes (or pass it as an argument)")
		title       = flag.String("title", "", "document title for sourceRef (default: file name)")
		model       = flag.String("model", "", "model, overrides GIGACHAT_MODEL")
		chunk       = flag.Int("chunk", cards.DefaultChunkSize, "fragment size in characters")
		overlap     = flag.Int("overlap", cards.DefaultChunkOverlap, "fragment overlap in characters, -1 for none")
		limit       = flag.Int("limit", 0, "max cards per document, 0 for no limit")
		perChunk    = flag.Int("per-chunk", cards.DefaultMaxCardsPerChunk, "max cards per fragment")
		concurrency = flag.Int("concurrency", cards.DefaultConcurrency, "parallel model calls; freemium allows 1")
		timeout     = flag.Duration("call-timeout", cards.DefaultCallTimeout, "timeout per model call")
		asJSON      = flag.Bool("json", false, "print the result as JSON to stdout")
		verbose     = flag.Bool("v", false, "debug logs")
	)
	flag.Parse()
	if *file == "" && flag.NArg() > 0 {
		*file = flag.Arg(0)
	}
	if *file == "" {
		flag.Usage()
		return errors.New("no input file")
	}

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	for _, path := range []string{".env", "../.env"} {
		if err := loadDotEnv(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %w", path, err)
		}
	}

	text, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("read notes: %w", err)
	}
	if *title == "" {
		*title = strings.TrimSuffix(filepath.Base(*file), filepath.Ext(*file))
	}

	cfg := gigachat.ConfigFromEnv()
	cfg.Logger = logger
	if *model != "" {
		cfg.Model = *model
	}
	client, err := gigachat.New(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	began := time.Now()
	delivered := 0
	gen := cards.New(client, cards.Options{
		Concurrency:      *concurrency,
		ChunkSize:        *chunk,
		ChunkOverlap:     *overlap,
		MaxCardsPerChunk: *perChunk,
		MaxCardsPerDoc:   *limit,
		CallTimeout:      *timeout,
		Logger:           logger,
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

	fmt.Fprintf(os.Stderr, "%s: %d символов, модель %s\n", *file, len([]rune(string(text))), or(cfg.Model, gigachat.DefaultModel))
	res, genErr := gen.Generate(ctx, cards.Document{Text: string(text), Title: *title})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(res); err != nil {
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

func formatCards(list []cards.Card) string {
	var w strings.Builder
	for i, c := range list {
		fmt.Fprintf(&w, "\n#%d [%s] %s — %s\n", i+1, c.Kind, c.Topic, c.SourceRef)
		fmt.Fprintf(&w, "  В: %s\n", c.Question)
		if len(c.Options) > 0 {
			opts := make([]string, len(c.Options))
			for j, o := range c.Options {
				opts[j] = o
				if o == c.Answer {
					opts[j] += " ✓"
				}
			}
			fmt.Fprintf(&w, "  Варианты: %s\n", strings.Join(opts, " | "))
		}
		fmt.Fprintf(&w, "  О: %s\n", c.Answer)
		fmt.Fprintf(&w, "  Объяснение: %s\n", c.Explanation)
		fmt.Fprintf(&w, "  Цитата: «%s»\n", c.SourceQuote)
	}
	return w.String()
}

func formatStats(s cards.Stats) string {
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
	fmt.Fprintf(&w, "Карточек от модели:    %d\n", s.CardsFromModel)
	fmt.Fprintf(&w, "  цитата не найдена:   %d (%s)\n", s.DroppedQuote, pct(s.DroppedQuote))
	fmt.Fprintf(&w, "  дубликаты:           %d (%s)\n", s.DroppedDuplicate, pct(s.DroppedDuplicate))
	fmt.Fprintf(&w, "  не по схеме:         %d (%s)\n", s.DroppedInvalid, pct(s.DroppedInvalid))
	fmt.Fprintf(&w, "  сверх лимита:        %d (%s)\n", s.DroppedByLimit, pct(s.DroppedByLimit))
	fmt.Fprintf(&w, "  choice → flip:       %d (мало дистракторов)\n", s.DowngradedToFlip)
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

// loadDotEnv sets KEY=VALUE pairs from a .env file, keeping variables that
// are already set.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = dotEnvValue(value)
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// dotEnvValue unquotes a value in matching quotes and drops a trailing
// " # comment" from an unquoted one: "GigaChat-3-Ultra # main" must not
// become a model name.
func dotEnvValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : end+1]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
