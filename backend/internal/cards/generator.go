package cards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// answerAttempts is the first call plus one retry for an answer that is
	// not valid JSON or breaks the schema.
	answerAttempts = 2
	// maxConsecutiveFailures stops the document when the provider fails on
	// this many fragments in a row, after its own retries and fallback model:
	// bad credentials or an outage, not a flaky fragment.
	maxConsecutiveFailures = 3
	// Completion budget: a fact with its quote and four cards is about 600
	// tokens; the margin keeps a verbose answer from being cut mid-JSON.
	tokensPerFact   = 900
	tokensReserve   = 200
	logAnswerLength = 300
)

// Generator makes cards from documents. It is safe for concurrent use.
type Generator struct {
	provider Provider
	opts     Options
	system   string
	schema   json.RawMessage
}

// New returns a Generator. Zero Options fields take defaults.
func New(p Provider, opts Options) *Generator {
	opts = opts.withDefaults()
	return &Generator{
		provider: p,
		opts:     opts,
		system:   buildSystemPrompt(opts.MaxFactsPerChunk),
		schema:   cardSchema(opts.MaxFactsPerChunk),
	}
}

// Generate runs the pipeline on one document: normalize, split, drop junk,
// one model call per fragment, check quotes, drop duplicates, add
// distractors, trim to MaxFactsPerDoc. Cards reach OnCards as they are ready;
// the Result holds all of them in document order.
//
// A fragment that fails does not fail the document. An error is returned when
// ctx is cancelled, when the provider fails on maxConsecutiveFailures
// fragments in a row, or when it fails on every fragment; the Result then
// holds what was made before.
func (g *Generator) Generate(ctx context.Context, doc Document) (Result, error) {
	began := time.Now()
	log := g.opts.Logger

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

	asm := newAssembler(doc.Title, len(work), g.opts.MaxFactsPerDoc, &stats)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	slots := make(chan struct{}, g.opts.Concurrency)
	results := make(chan chunkResult, len(work))
	go g.dispatch(runCtx, doc.Title, len(all), work, asm, slots, results)

	deliver := func(cards []Card) {
		if len(cards) == 0 {
			return
		}
		if stats.FirstCardAfter == 0 {
			stats.FirstCardAfter = time.Since(began)
		}
		g.notify(cards)
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
			case r.skipped || r.cancelled:
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
				// dispatch sees these cards and a stop, and before OnCards,
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
	result.Stats.Cards = len(result.Cards)
	result.Stats.Facts = countFacts(result.Cards)
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
func (g *Generator) dispatch(ctx context.Context, title string, total int, work []chunk,
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
			results <- g.processChunk(ctx, title, total, r)
		})
	}
}

// processChunk makes the model call for one fragment, retries once on an
// unparseable answer and checks every quote against the fragment.
func (g *Generator) processChunk(ctx context.Context, title string, total int, r chunkResult) chunkResult {
	began := time.Now()
	log := g.opts.Logger.With("fragment", r.chunk.Index+1)
	r.models = map[string]int{}

	req := Request{
		System:      g.system,
		Schema:      g.schema,
		Temperature: 0,
		MaxTokens:   g.opts.MaxFactsPerChunk*tokensPerFact + tokensReserve,
	}
	var parsed []modelFact
	answered := false
	for attempt := range answerAttempts {
		req.User = buildUserPrompt(title, r.chunk, total, attempt > 0)
		callCtx, cancel := context.WithTimeout(ctx, g.opts.CallTimeout)
		resp, err := g.provider.Complete(callCtx, req)
		cancel()
		r.calls++
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
	r.invalid = r.err == nil && !answered
	r.factsFromModel = len(parsed)
	r.cardsFromModel = r.droppedInvalid
	for _, f := range parsed {
		r.cardsFromModel += len(f.Cards)
	}

	// One quote per fact: a fact that fails the check takes all its cards
	// with it.
	for _, f := range parsed {
		quote, ok := findQuote(r.chunk.Text, f.Quote)
		if !ok {
			r.droppedQuote += len(f.Cards)
			log.Info("cards: quote not found in fragment", "topic", f.Topic, "quote", f.Quote)
			continue
		}
		f.Quote = quote
		f.ID = factID(quote)
		r.facts = append(r.facts, f)
	}
	r.elapsed = time.Since(began)
	return r
}

// notify hands a batch to OnCards. A panic there is the caller's bug and
// must not take the rest of the document down with it.
func (g *Generator) notify(cards []Card) {
	if g.opts.OnCards == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			g.opts.Logger.Error("cards: OnCards panicked", "panic", p)
		}
	}()
	g.opts.OnCards(slices.Clone(cards))
}

// factID derives a fact's id from its verified quote, so the same fact met
// again in an overlapping fragment gets the same id.
func factID(quote string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fold(quote).s))
	return fmt.Sprintf("%016x", h.Sum64())
}

func countFacts(cards []Card) int {
	seen := map[string]bool{}
	for _, c := range cards {
		seen[c.FactID] = true
	}
	return len(seen)
}

func clip(b []byte, n int) string {
	r := []rune(string(b))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
