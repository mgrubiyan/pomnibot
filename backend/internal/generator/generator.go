// Package generator makes validated study cards from complete source documents.
package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// PipelineVersion identifies source input, prompt and code validation semantics.
const PipelineVersion = "direct-generation-v12"

// Generator is safe for concurrent use. The provider owns transport retries.
type Generator struct {
	provider providers.Provider
	opts     Options
}

// NewGenerator returns the single document generator.
func NewGenerator(p providers.Provider, opts Options) *Generator {
	return &Generator{provider: p, opts: opts.withDefaults()}
}

// WithOnBatch returns a copy with a caller-specific progress callback.
func (g *Generator) WithOnBatch(cb func(Batch)) *Generator {
	if g == nil {
		return nil
	}
	cp := *g
	cp.opts.OnBatch = cb
	return &cp
}

// Generate creates facts and cards together from the complete source.
// It publishes atomically after structural validation, not semantic verification.
func (g *Generator) Generate(ctx context.Context, doc Document) (out Result, err error) {
	if providers.BudgetFrom(ctx) == nil {
		ctx = providers.WithBudget(ctx, providers.NewBudget(g.opts.Budget))
	}
	defer func() { out.Budget = providers.BudgetFrom(ctx).Snapshot() }()
	started := time.Now()
	defer func() {
		out.Stats.Total = time.Since(started)
		if err != nil {
			var detail *diagnosticError
			if !errors.As(err, &detail) || detail.stage == "" {
				err = contextualProblem(err, "preflight", g.opts, out.Stats.Total)
			}
		}
	}()
	if g.opts.MaxTokens < 1 {
		return out, problem("configuration", "maxTokens", "generation output limit must be positive")
	}
	switch g.opts.ReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return out, problem("configuration", "reasoningEffort", "generation requires supported reasoning effort")
	}
	pages := max(1, len(doc.PageStarts))
	nextPage := 1
	doc.PDFs = slices.Clone(doc.PDFs)
	for i := range doc.PDFs {
		pdf := &doc.PDFs[i]
		if pdf.Pages < 1 {
			return out, problem("preflight", fmt.Sprintf("pdfs[%d].pages", i), "PDF page count must be positive")
		}
		if len(pdf.Data) == 0 {
			return out, problem("preflight", fmt.Sprintf("pdfs[%d].data", i), "PDF data is empty")
		}
		if pdf.StartPage < 1 {
			pdf.StartPage = nextPage
		}
		nextPage = pdf.StartPage + pdf.Pages
		pages = max(pages, nextPage-1)
	}

	source, sourcePages, e := generationSource(doc)
	if e != nil {
		return out, e
	}
	pages = max(pages, sourcePages)
	if len(source) > g.opts.SourceBudget {
		detail := problem("limit", "source", fmt.Sprintf("complete source needs %d bytes, budget is %d", len(source), g.opts.SourceBudget))
		detail.cause = providers.ErrCapability
		return out, detail
	}
	out.Stats.Stages = map[string]StageStats{}
	out.Stats.CallsByModel = map[string]int{}
	out.Artifact = &Artifact{Version: PipelineVersion, Stages: map[string]json.RawMessage{}}
	r, e := g.completeStage(ctx, doc, source, "generation", map[string]any{"task": prompts.GenerateGoals, "title": doc.Title}, pages, &out)
	if e != nil {
		return out, e
	}
	out.Facts = r.Facts
	out.Artifact.Facts = r.Facts
	out.Stats.Cards = CardCount(r.Facts)
	for _, fact := range r.Facts {
		for _, card := range fact.Cards {
			switch card.(type) {
			case FlipCard, InputCard:
				out.Stats.CardsSynthesized++
			default:
				out.Stats.CardsFromModel++
			}
		}
	}
	out.Stats.Facts = len(r.Facts)
	out.Stats.FirstCardAfter = time.Since(started)
	g.notify(Batch{Facts: out.Facts})
	return out, nil
}

func (g *Generator) notify(b Batch) {
	if g.opts.OnBatch == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("cards: OnBatch panicked", "panic", p)
		}
	}()
	g.opts.OnBatch(Batch{Facts: cloneFacts(b.Facts)})
}

// Clone nested slices so a progress callback cannot mutate the validated result.
func cloneFacts(facts []Fact) []Fact {
	out := slices.Clone(facts)
	for i := range out {
		out[i].Pages = slices.Clone(out[i].Pages)
		out[i].cardIndices = slices.Clone(out[i].cardIndices)
		out[i].cardPaths = slices.Clone(out[i].cardPaths)
		out[i].Cards = slices.Clone(out[i].Cards)
		for j, c := range out[i].Cards {
			switch v := c.(type) {
			case ChoiceCard:
				v.RightAnswers = slices.Clone(v.RightAnswers)
				v.WrongAnswers = slices.Clone(v.WrongAnswers)
				out[i].Cards[j] = v
			case TableCard:
				v.Columns = slices.Clone(v.Columns)
				for k := range v.Columns {
					v.Columns[k].Rows = slices.Clone(v.Columns[k].Rows)
				}
				out[i].Cards[j] = v
			}
		}
	}
	return out
}

func (g *Generator) completeStage(ctx context.Context, doc Document, source, stage string, input any, pages int, out *Result) (reply AIResponse, callErr error) {
	began := time.Now()
	before := providers.BudgetFrom(ctx).Snapshot()
	payload, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, g.opts.CallTimeout)
	defer cancel()
	req := providers.Request{Source: source, NoFallback: true, System: prompts.GeneratorSystem, User: string(payload), PDFs: doc.PDFs, Schema: stageSchema(), ReasoningEffort: g.opts.ReasoningEffort, MaxTokens: g.opts.MaxTokens, NoCache: true}
	out.Artifact.Request = &req
	resp, e := g.provider.Complete(callCtx, req)
	out.Artifact.Response = string(resp.Content)
	if json.Valid(resp.Content) {
		out.Artifact.Stages[stage] = resp.Content
	}
	// Usage includes transport attempts; a failed or truncated answer is still billed.
	st := StageStats{UnreportedUsageCalls: resp.Usage.UnreportedCalls, ModelCalls: max(1, resp.Usage.Attempts), PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, ReasoningTokens: resp.Usage.ReasoningTokens, CostUSD: resp.Usage.CostUSD}
	if resp.Cached {
		st.ModelCalls = 0
		st.CacheHits = 1
	}
	if errors.Is(e, providers.ErrBudget) && resp.Usage.Attempts == 0 {
		st.ModelCalls = 0
	}
	if resp.Usage.CachedTokens != nil {
		st.CacheUsageReported = true
		st.CachedTokens = *resp.Usage.CachedTokens
		if st.CachedTokens > 0 {
			st.CacheHits = 1
		}
	}
	if resp.Usage.CacheWriteTokens != nil {
		st.CacheWriteTokens = *resp.Usage.CacheWriteTokens
	}
	if e != nil {
		e = providerProblem(e)
	} else {
		e = decodeResponse(resp, &reply)
		if e != nil {
			st.InvalidResponses = 1
		}
	}
	st.FailedCalls = resp.Usage.FailedAttempts
	if e != nil && st.FailedCalls == 0 {
		st.FailedCalls = 1
	}
	// Validate before publishing stage stats so its Error includes failures after
	// decoding. Keep existing call/billing counters for these validation failures.
	if e == nil {
		out.Coverage.Issues = slices.Clone(reply.Issues)
		e = validateFacts(reply.Facts, pages)
		if e == nil && g.opts.MaxFactsPerDoc > 0 && len(reply.Facts) > g.opts.MaxFactsPerDoc {
			e = problem("limit", "facts", fmt.Sprintf("generated %d facts, configured limit is %d", len(reply.Facts), g.opts.MaxFactsPerDoc))
		}
		if e != nil {
			st.InvalidResponses = 1
		}
	}
	st.Model = resp.Model
	st.Duration = time.Since(began)
	if e != nil {
		detail := contextualProblem(e, stage, g.opts, st.Duration)
		// A zero-value response on transport failure provides no observation of
		// response length or finish status. An empty successful response does.
		detail.addResponse(resp, resp.Content != nil || resp.Truncated || st.InvalidResponses > 0)
		e = detail
		st.Error = e.Error()
	}
	after := providers.BudgetFrom(ctx).Snapshot()
	after.Calls -= before.Calls
	after.CommittedUSD -= before.CommittedUSD
	after.ReportedUSD -= before.ReportedUSD
	st.Budget = after
	out.Stats.recordStage(stage, st)
	slog.Info("generation stage completed", "stage", stage, "stats", st)
	return reply, e
}

// generationSource keeps non-PDF extraction intact and the shared prompt prefix stable.
func generationSource(doc Document) (string, int, error) {
	pages := max(1, len(doc.PageStarts))
	source := map[string]any{"title": doc.Title}
	if doc.Structure != nil {
		source["blocks"] = doc.Structure.Blocks
		for _, block := range doc.Structure.Blocks {
			pages = max(pages, block.Page)
		}
		type pageIssues struct {
			Number int      `json:"number"`
			Issues []string `json:"issues"`
		}
		var issues []pageIssues
		for _, page := range doc.Structure.Pages {
			pages = max(pages, page.Number)
			if len(page.Issues) > 0 {
				issues = append(issues, pageIssues{page.Number, page.Issues})
			}
		}
		source["pageIssues"] = issues
	} else if strings.TrimSpace(doc.Text) != "" {
		starts := doc.PageStarts
		if len(starts) == 0 {
			starts = []int{0}
		}
		previous := -1
		for i, start := range starts {
			if start < 0 || start > len(doc.Text) {
				return "", 0, problem("preflight", fmt.Sprintf("pageStarts[%d]", i), "page offset is outside source bounds")
			}
			if start <= previous {
				return "", 0, problem("preflight", fmt.Sprintf("pageStarts[%d]", i), "page offsets must be strictly increasing")
			}
			previous = start
		}
		if starts[0] != 0 {
			return "", 0, problem("preflight", "pageStarts[0]", "page offsets omit source prefix")
		}
		var blocks []material.Block
		for i, start := range starts {
			end := len(doc.Text)
			if i+1 < len(starts) {
				end = starts[i+1]
			}
			blocks = append(blocks, material.Block{ID: fmt.Sprintf("p%d", i+1), Page: i + 1, Kind: "text", Text: doc.Text[start:end]})
		}
		source["blocks"] = blocks
	}

	if len(doc.PDFs) == 0 && (doc.Structure == nil || len(doc.Structure.Blocks) == 0) && strings.TrimSpace(doc.Text) == "" {
		return "", 0, problem("preflight", "", "document is empty")
	}
	data, e := json.Marshal(source)
	return string(data), pages, e
}
