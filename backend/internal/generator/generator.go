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
const PipelineVersion = "direct-generation-v11"

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
	defer func() { out.Stats.Total = time.Since(started) }()
	if g.opts.MaxTokens < 1 {
		return out, errors.New("invalid generation output limit")
	}
	switch g.opts.ReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return out, errors.New("generation requires supported reasoning effort")
	}
	pages := max(1, len(doc.PageStarts))
	nextPage := 1
	doc.PDFs = slices.Clone(doc.PDFs)
	for i := range doc.PDFs {
		pdf := &doc.PDFs[i]
		if pdf.Pages < 1 || len(pdf.Data) == 0 {
			return out, errors.New("invalid PDF source")
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
		return out, fmt.Errorf("complete source needs %d bytes, budget is %d: %w", len(source), g.opts.SourceBudget, providers.ErrCapability)
	}
	out.Stats.Stages = map[string]StageStats{}
	out.Stats.CallsByModel = map[string]int{}
	out.Artifact = &Artifact{Version: PipelineVersion, Stages: map[string]json.RawMessage{}}
	r, e := g.completeStage(ctx, doc, source, "generation", map[string]any{"task": prompts.GenerateGoals, "title": doc.Title}, &out)
	if e != nil {
		return out, e
	}
	out.Coverage.Issues = slices.Clone(r.Issues)
	if e = validateFacts(r.Facts, pages); e != nil {
		return out, invalidStage(&out, "generation", e)
	}
	if g.opts.MaxFactsPerDoc > 0 && len(r.Facts) > g.opts.MaxFactsPerDoc {
		return out, invalidStage(&out, "generation", errors.New("generated facts exceed configured fact limit"))
	}
	out.Facts = r.Facts
	out.Artifact.Facts = r.Facts
	out.Stats.Cards = CardCount(r.Facts)
	out.Stats.CardsFromModel = out.Stats.Cards
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

func (g *Generator) completeStage(ctx context.Context, doc Document, source, stage string, input any, out *Result) (reply AIResponse, callErr error) {
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
	st := StageStats{UnreportedUsageCalls: resp.Usage.UnreportedCalls, ModelCalls: max(1, resp.Usage.Attempts), PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, ReasoningTokens: resp.Usage.ReasoningTokens, CostUSD: resp.Usage.CostUSD, Duration: time.Since(began)}
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
	if e == nil && (resp.Truncated || !json.Valid(resp.Content) || strictJSON(resp.Content, &reply) != nil || reply.Issues == nil || reply.Facts == nil) {
		e = errors.New("invalid or unfinished structured response")
		st.InvalidResponses = 1
	}
	st.FailedCalls = resp.Usage.FailedAttempts
	if e != nil && st.FailedCalls == 0 {
		st.FailedCalls = 1
	}
	st.Model = resp.Model
	out.Stats.recordStage(stage, st)
	after := providers.BudgetFrom(ctx).Snapshot()
	after.Calls -= before.Calls
	after.CommittedUSD -= before.CommittedUSD
	after.ReportedUSD -= before.ReportedUSD
	st.Budget = after
	if e != nil {
		st.Error = e.Error()
	}
	out.Stats.Stages[stage] = st
	slog.Info("generation stage completed", "stage", stage, "stats", st)
	return reply, e
}

func invalidStage(out *Result, stage string, e error) error {
	st := out.Stats.Stages[stage]
	st.Error = e.Error()
	st.InvalidResponses++
	out.Stats.Stages[stage] = st
	out.Stats.InvalidResponses++
	return fmt.Errorf("generation %s: %w", stage, e)
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
		for _, start := range starts {
			if start < 0 || start > len(doc.Text) || start <= previous {
				return "", 0, errors.New("invalid page offsets")
			}
			previous = start
		}
		if starts[0] != 0 {
			return "", 0, errors.New("page offsets omit source prefix")
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
		return "", 0, errors.New("empty document")
	}
	data, e := json.Marshal(source)
	return string(data), pages, e
}
