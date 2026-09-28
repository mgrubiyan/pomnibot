// Package generator turns lecture notes into self-check cards.
//
// The pipeline never trusts the model with facts: every card must carry a
// verbatim quote from the fragment it was generated from, and the quote is
// checked in code. Wrong options for choice cards are taken from answers to
// other fragments of the same document, never invented. See docs/concept.md,
// section 6.
//
// The package is storage-agnostic: text in, cards (models/cards) and stats
// out. The model is behind providers.Provider, so GigaChat, YandexGPT or any
// OpenAI-compatible API plug in without changes here:
//
//	cfg, err := gigachat.ConfigFromEnv()
//	llm, err := gigachat.New(cfg)
//	gen := generator.NewGenerator(llm, generator.Options{OnBatch: save})
//	res, err := gen.Generate(ctx, generator.Document{Text: text, Title: title, ID: setID})
package generator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const (
	// answerAttempts is the first call plus one retry for an answer that is
	// not valid JSON or breaks the schema.
	answerAttempts = 2
	// maxConsecutiveFailures stops the document when the provider fails on
	// this many fragments in a row, after its own retries and fallback model:
	// bad credentials or an outage, not a flaky fragment.
	maxConsecutiveFailures = 3
	logAnswerLength        = 300
)

// Generator makes cards from documents. It is safe for concurrent use.
type Generator struct {
	provider providers.Provider
	opts     Options
	system   string
	schema   json.RawMessage
}

// NewGenerator returns a Generator. Zero Options fields take defaults.
func NewGenerator(p providers.Provider, opts Options) *Generator {
	opts = opts.withDefaults()
	return &Generator{
		provider: p,
		opts:     opts,
		system:   buildSystemPrompt(opts.MaxFactsPerChunk),
		schema:   cardSchema(opts.MaxFactsPerChunk),
	}
}

// WithOnBatch returns a shallow copy of Generator with a custom OnBatch callback.
func (g *Generator) WithOnBatch(onBatch func(Batch)) *Generator {
	if g == nil {
		return nil
	}
	cp := *g
	cp.opts.OnBatch = onBatch
	return &cp
}

// Generate runs the pipeline on one document: normalize, split, drop junk,
// one model call per fragment, check quotes, drop duplicates, add
// distractors, trim to MaxFactsPerDoc. Cards reach OnBatch as they are ready;
// the Result holds all of them in document order.
//
// A fragment that fails does not fail the document. An error is returned when
// ctx is cancelled, when the provider fails on maxConsecutiveFailures
// fragments in a row, or when it fails on every fragment; the Result then
// holds what was made before.
func (g *Generator) Generate(ctx context.Context, doc Document) (Result, error) {
	began := time.Now()
	log := slog.Default()

	if doc.ID == "" {
		doc.ID = rand.Text()
	}
	text := normalizeText(doc.Text)
	all := splitChunks(text, g.opts.ChunkSize, g.opts.ChunkOverlap)
	stats := Stats{
		Chunks:       len(all),
		FilteredBy:   map[string]int{},
		CallsByModel: map[string]int{},
	}
	var work []chunk
	for _, c := range all {
		if reason := junkReason(c.Text); reason != "" {
			stats.ChunksFiltered++
			stats.FilteredBy[reason]++
			log.Debug("cards: fragment filtered", "fragment", c.Index+1, "reason", reason)
			continue
		}
		work = append(work, c)
	}
	log.Info("cards: document split",
		"title", doc.Title, "chars", utf8.RuneCountInString(text),
		"fragments", len(all), "filtered", stats.ChunksFiltered)

	asm := newAssembler(doc.Title, normalizedPageStarts(doc.Text, doc.PageStarts, text), len(work), g.opts.MaxFactsPerDoc, &stats)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	slots := make(chan struct{}, g.opts.Concurrency)
	results := make(chan chunkResult, len(work))
	go g.dispatch(runCtx, doc, len(all), work, asm, slots, results)

	deliver := func(cs []cards.Card) {
		if len(cs) == 0 {
			return
		}
		if stats.FirstCardAfter == 0 {
			stats.FirstCardAfter = time.Since(began)
		}
		g.notify(Batch{Facts: asm.newFacts(cs), Cards: cs})
	}

	var (
		runErr   error
		failures int
		okChunks int
		next     int
		waiting  = map[int]chunkResult{}
	)
	// Workers finish in any order; results are assembled in fragment order.
	for r := range results {
		waiting[r.pos] = r
		for {
			r, ok := waiting[next]
			if !ok {
				break
			}
			delete(waiting, next)
			next++

			ready := asm.add(r)
			switch {
			case r.skipped || r.cancelled || r.refused:
			case r.err != nil:
				failures++
				log.Warn("cards: fragment failed", "fragment", r.chunk.Index+1, "err", r.err)
				if failures >= maxConsecutiveFailures && runErr == nil {
					runErr = fmt.Errorf("provider failed on %d fragments in a row: %w", failures, r.err)
					cancel()
				}
			default:
				failures = 0
				okChunks++
			}
			if r.holdsSlot {
				// Released after assembly and the failure check, so the next
				// dispatch sees these cards and a stop, and before OnBatch,
				// so a slow callback does not hold up the next model call.
				<-slots
			}
			deliver(ready)
		}
	}
	deliver(asm.finish())

	if runErr == nil && ctx.Err() != nil {
		runErr = fmt.Errorf("generate: %w", ctx.Err())
	}
	if runErr == nil && stats.ChunksFailed > 0 && okChunks == 0 {
		runErr = errors.New("provider failed on every fragment")
	}

	result := Result{Cards: asm.cards(), Stats: stats}
	result.Facts = asm.factsOf(result.Cards)
	result.Stats.Cards = len(result.Cards)
	result.Stats.Facts = len(result.Facts)
	result.Stats.Total = time.Since(began)
	log.Info("cards: document done",
		"title", doc.Title, "cards", result.Stats.Cards,
		"from_model", result.Stats.CardsFromModel, "dropped_quote", result.Stats.DroppedQuote,
		"calls", result.Stats.ModelCalls, "took", result.Stats.Total.Round(time.Millisecond),
		"err", runErr)
	return result, runErr
}

// dispatch starts fragments in document order, at most Concurrency at a time.
// Every fragment gets exactly one result, so the ordered assembly never
// waits for a gap. Slots are released by the assembler, not by the worker.
func (g *Generator) dispatch(ctx context.Context, doc Document, total int, work []chunk,
	asm *assembler, slots chan struct{}, results chan<- chunkResult) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(results)
	}()

	for pos, c := range work {
		r := chunkResult{pos: pos, chunk: c}
		acquired := false
		select {
		case slots <- struct{}{}:
			acquired = true
		case <-ctx.Done():
		}
		// select picks at random when both are ready: check the stop again.
		if ctx.Err() != nil || asm.budgetSpent(pos) {
			if acquired {
				<-slots
			}
			r.cancelled = ctx.Err() != nil
			r.skipped = !r.cancelled
			results <- r
			continue
		}
		r.holdsSlot = true
		wg.Go(func() {
			results <- g.processChunk(ctx, doc, total, r)
		})
	}
}

// processChunk makes the model call for one fragment, retries once on an
// unparseable answer and checks every quote against the fragment.
func (g *Generator) processChunk(ctx context.Context, doc Document, total int, r chunkResult) chunkResult {
	began := time.Now()
	log := slog.With("fragment", r.chunk.Index+1)
	r.models = map[string]int{}

	req := providers.Request{
		System:      g.system,
		Schema:      g.schema,
		Temperature: 0,
	}
	var parsed []modelFact
	answered := false
	for attempt := range answerAttempts {
		req.User = buildUserPrompt(doc.Title, r.chunk, total, attempt > 0)
		callCtx, cancel := context.WithTimeout(ctx, g.opts.CallTimeout)
		resp, err := g.provider.Complete(callCtx, req)
		cancel()
		r.calls++
		if errors.Is(err, providers.ErrRefused) {
			// About this text only: asking again gets the same, and the
			// rest of the document may pass.
			r.refused = true
			log.Warn("cards: fragment refused by the content filter", "err", err)
			break
		}
		if err != nil {
			r.cancelled = ctx.Err() != nil
			r.err = fmt.Errorf("fragment %d: %w", r.chunk.Index+1, err)
			break
		}

		r.usage.PromptTokens += resp.Usage.PromptTokens
		r.usage.CompletionTokens += resp.Usage.CompletionTokens
		model := resp.Model
		if model == "" {
			model = "unknown"
		}
		r.models[model]++

		facts, invalid, err := parseAnswer(resp.Content, g.opts.MaxFactsPerChunk)
		if err != nil {
			r.invalidResponses++
			log.Warn("cards: invalid model answer",
				"attempt", attempt+1, "err", err, "answer", clip(resp.Content, logAnswerLength))
			continue
		}
		parsed, r.droppedInvalid, answered = facts, invalid, true
		break
	}
	r.invalid = r.err == nil && !r.refused && !answered
	r.factsFromModel = len(parsed)
	r.cardsFromModel = r.droppedInvalid
	for _, f := range parsed {
		r.cardsFromModel += len(f.Cards)
	}

	// One quote per fact: a fact that fails the check takes all its cards
	// with it.
	for _, f := range parsed {
		start, end, ok := locateQuote(r.chunk.Text, f.Quote)
		if !ok {
			r.droppedQuote += len(f.Cards)
			log.Info("cards: quote not found in fragment", "topic", f.Topic, "quote", f.Quote)
			continue
		}
		f.Quote = r.chunk.Text[start:end]
		f.Start, f.End = r.chunk.Start+start, r.chunk.Start+end
		f.ID = factID(doc.ID, f.Quote)

		kept := f.Cards[:0]
		for _, c := range f.Cards {
			if answerChecked(c.Kind) && !answerSupported(c, f.Quote) {
				r.droppedUnsupported++
				log.Info("cards: answer not in the quote or given in the question",
					"kind", c.Kind, "question", c.Question, "answer", c.Answer)
				continue
			}
			kept = append(kept, c)
		}
		if f.Cards = kept; len(f.Cards) > 0 {
			r.facts = append(r.facts, f)
		}
	}
	r.elapsed = time.Since(began)
	return r
}

// notify hands a batch to OnBatch. A panic there is the caller's bug and
// must not take the rest of the document down with it.
func (g *Generator) notify(b Batch) {
	if g.opts.OnBatch == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("cards: OnBatch panicked", "panic", p)
		}
	}()
	g.opts.OnBatch(Batch{Facts: slices.Clone(b.Facts), Cards: slices.Clone(b.Cards)})
}

// factID derives a fact's id from the document and the verified quote, so the
// same fact met again in an overlapping fragment gets the same id, and the
// same quote in another document does not.
func factID(docID, quote string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(docID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(fold(quote).s))
	return fmt.Sprintf("%016x", h.Sum64())
}

func clip(b []byte, n int) string {
	r := []rune(string(b))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
