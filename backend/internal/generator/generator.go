// Package generator makes validated study cards from complete source documents.
package generator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"hash/fnv"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// PipelineVersion identifies source input, prompt and code validation semantics.
const PipelineVersion = "direct-generation-v9"

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

// Generate creates goals and cards together from the complete source.
// It publishes atomically after structural validation, not semantic verification.
func (g *Generator) Generate(ctx context.Context, doc Document) (out Result, err error) {
	if providers.BudgetFrom(ctx) == nil {
		ctx = providers.WithBudget(ctx, providers.NewBudget(g.opts.Budget))
	}
	defer func() { out.Budget = providers.BudgetFrom(ctx).Snapshot() }()
	started := time.Now()
	defer func() { out.Stats.Total = time.Since(started) }()
	if doc.ID == "" {
		doc.ID = rand.Text()
	}
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
	goals, groups := r.Goals, r.Cards
	out.Coverage.Issues = slices.Clone(r.Issues)
	if e = validateGoals(goals, pages); e != nil {
		return out, invalidStage(&out, "generation", e)
	}
	if e = validateGoalCards(goals, groups); e != nil {
		return out, invalidStage(&out, "generation", e)
	}
	if g.opts.MaxFactsPerDoc > 0 && len(goals) > g.opts.MaxFactsPerDoc {
		return out, invalidStage(&out, "generation", errors.New("generated goals exceed configured fact limit"))
	}
	// Validate model references before replacing them with canonical server IDs.
	ids := map[string]string{}
	for i := range goals {
		old := goals[i].GoalID
		goals[i].GoalID = "g" + strconv.Itoa(i+1)
		ids[old] = goals[i].GoalID
		if quote := goals[i].SourceQuote; quote != "" && !sourceContainsQuote(doc, goals[i].Pages, quote) {
			return out, invalidStage(&out, "generation", errors.New("source quote is not present on cited text pages"))
		}
	}
	for i := range groups {
		groups[i].GoalID = ids[groups[i].GoalID]
	}
	byID := map[string]Fact{}
	for _, v := range groups {
		byID[v.GoalID] = v.Content
	}
	for _, v := range goals {
		id := factID(doc.ID, v.GoalID)
		out.Facts = append(out.Facts, cards.Fact{ID: id, Name: v.Name, Topic: v.Topic})
		refs := []string{}
		for _, p := range v.Pages {
			refs = append(refs, strconv.Itoa(p))
		}
		for _, raw := range byID[v.GoalID].Cards {
			c, _ := storedCard(raw)
			c.FactID = id
			c.SourceRef = doc.Title + ", стр. " + strings.Join(refs, ", ")
			c.Explanation = v.Source
			c.SourceQuote = v.SourceQuote
			out.Cards = append(out.Cards, c)
		}
	}
	out.Artifact.Goals = goals
	out.Stats.Cards = len(out.Cards)
	out.Stats.CardsFromModel = len(out.Cards)
	out.Stats.Facts = len(out.Facts)
	out.Stats.FirstCardAfter = time.Since(started)
	g.notify(Batch{Cards: out.Cards, Facts: out.Facts})
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
	g.opts.OnBatch(Batch{Cards: slices.Clone(b.Cards), Facts: slices.Clone(b.Facts)})
}

func factID(docID, key string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(docID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("%016x", h.Sum64())
}

func (g *Generator) completeStage(ctx context.Context, doc Document, source, stage string, input any, out *Result) (reply stageResponse, callErr error) {
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
	if e == nil && (resp.Truncated || !json.Valid(resp.Content) || strictJSON(resp.Content, &reply) != nil || reply.Issues == nil || reply.Goals == nil || reply.Cards == nil) {
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

// sourceContainsQuote accepts only verbatim fragments in the supplied text layer.
// Native PDFs without a text layer retain page references and source explanations.
func sourceContainsQuote(doc Document, pages []int, quote string) bool {
	cited := map[int]bool{}
	for _, p := range pages {
		cited[p] = true
	}
	if doc.Structure != nil {
		for _, b := range doc.Structure.Blocks {
			if cited[b.Page] && strings.Contains(b.Text, quote) {
				return true
			}
		}
		return false
	}
	starts := doc.PageStarts
	if len(starts) == 0 {
		starts = []int{0}
	}
	for i, start := range starts {
		end := len(doc.Text)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if cited[i+1] && strings.Contains(doc.Text[start:end], quote) {
			return true
		}
	}
	return false
}
