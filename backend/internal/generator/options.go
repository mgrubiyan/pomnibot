package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// Document is the input: plain text of the notes and their title.
//
// ID identifies the stored document, such as its set id. Fact ids are
// derived from it and the server-assigned goal ID: the same notes uploaded twice must not share
// fact ids, which the storage keeps unique across all sets. Without an ID
// every Generate call picks a random one.
//
// PageStarts are byte offsets in Text where pages 1, 2, … start, as
// ingest.PageInfo.Start gives them: cards then cite pages. Without them, as
// for plain text, cards cite the title only.
type Document struct {
	PDFs       []providers.PDF
	Structure  *material.Document
	Text       string
	Title      string
	ID         string
	PageStarts []int
}

// Artifact retains private originals and the source representation for auditing.
type Artifact struct {
	Stages  map[string]json.RawMessage `json:"stages,omitempty"`
	Goals   []KnowledgeGoal            `json:"goals,omitempty"`
	Version string                     `json:"version"`
	Reading material.Document          `json:"reading"`
	Files   []ingest.File              `json:"files"` // private originals for reproducible region references
}

// Result includes cards, available source coverage and processing metrics.
type Result struct {
	Budget providers.BudgetSnapshot `json:"budget"`

	Artifact *Artifact    `json:"artifact,omitempty"`
	Facts    []cards.Fact `json:"facts"`
	Cards    []cards.Card `json:"cards"`
	Stats    Stats        `json:"stats"`
	Coverage Coverage     `json:"coverage"`
}

// Batch is what OnBatch receives: cards now final and the facts first met
// among them. A fact comes once, in the batch with its first card, so the
// storage can insert it before its cards.
type Batch struct {
	Facts []cards.Fact
	Cards []cards.Card
}

// Coverage records source blocks without surviving citations and validation issues.
type Coverage struct {
	Issues     []string `json:"issues,omitempty"`
	Unassigned []string `json:"unassigned,omitempty"`
}

// Stats describes one Generate run.
type Stats struct {
	Stages               map[string]StageStats `json:"stages,omitempty"`
	CachedTokens         int                   `json:"cachedTokens"`
	CacheWriteTokens     int                   `json:"cacheWriteTokens"`
	UnreportedUsageCalls int                   `json:"unreportedUsageCalls"`
	CacheUsageReported   bool                  `json:"cacheUsageReported"`
	CacheHits            int                   `json:"cacheHits"`
	CostUSD              float64               `json:"costUsd"`
	ModelCalls           int                   `json:"modelCalls"`
	FailedCalls          int                   `json:"failedCalls"`
	InvalidResponses     int                   `json:"invalidResponses"`
	CallsByModel         map[string]int        `json:"callsByModel"`
	PromptTokens         int                   `json:"promptTokens"`
	CompletionTokens     int                   `json:"completionTokens"`
	ReasoningTokens      *int                  `json:"reasoningTokens,omitempty"`
	Facts                int                   `json:"facts"`

	// CardsFromModel = DroppedInvalid + DroppedQuote + DroppedDuplicate + DroppedByLimit + Cards.
	CardsFromModel   int `json:"cardsFromModel"`
	DroppedInvalid   int `json:"droppedInvalid"`
	DroppedQuote     int `json:"droppedQuote"`
	DroppedDuplicate int `json:"droppedDuplicate"`
	DroppedByLimit   int `json:"droppedByLimit"`
	Cards            int `json:"cards"`

	FirstCardAfter time.Duration `json:"firstCardAfter"`
	Total          time.Duration `json:"total"`
}

// DefaultCallTimeout bounds one generation stage, including transport retries.
const DefaultCallTimeout = 15 * time.Minute

// Options controls generation stages and their shared document budget.
type Options struct {
	ReasoningEffort string
	MaxTokens       int
	Budget          providers.BudgetLimits
	SourceBudget    int // complete source bytes; zero defaults to 80000
	MaxFactsPerDoc  int // zero means unlimited
	CallTimeout     time.Duration
	OnBatch         func(Batch)
}

func (o Options) withDefaults() Options {
	if o.MaxTokens == 0 {
		o.MaxTokens = 128000
	}
	if o.ReasoningEffort == "" {
		o.ReasoningEffort = "medium"
	}
	if o.Budget == (providers.BudgetLimits{}) {
		o.Budget = providers.DefaultDocumentLimits()
	}
	if o.SourceBudget <= 0 {
		o.SourceBudget = 80000
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = DefaultCallTimeout
	}
	return o
}

// OptionsFromEnv configures reasoning and output limits for generation stages.
func OptionsFromEnv() (Options, error) {
	o := Options{ReasoningEffort: os.Getenv("GENERATION_REASONING_EFFORT")}
	if raw := os.Getenv("GENERATION_MAX_TOKENS"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n <= 0 {
			return o, fmt.Errorf("GENERATION_MAX_TOKENS must be positive")
		}
		o.MaxTokens = n
	}
	o = o.withDefaults()
	switch o.ReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return o, fmt.Errorf("GENERATION_REASONING_EFFORT must enable reasoning")
	}
	return o, nil
}

// StageStats records transport attempts, billing and validation for one phase.
type StageStats struct {
	Model                string                   `json:"model"`
	ModelCalls           int                      `json:"modelCalls"`
	FailedCalls          int                      `json:"failedCalls"`
	InvalidResponses     int                      `json:"invalidResponses"`
	PromptTokens         int                      `json:"promptTokens"`
	CompletionTokens     int                      `json:"completionTokens"`
	ReasoningTokens      *int                     `json:"reasoningTokens,omitempty"`
	CostUSD              float64                  `json:"costUsd"`
	CacheHits            int                      `json:"cacheHits"`
	CachedTokens         int                      `json:"cachedTokens"`
	CacheWriteTokens     int                      `json:"cacheWriteTokens"`
	UnreportedUsageCalls int                      `json:"unreportedUsageCalls"`
	CacheUsageReported   bool                     `json:"cacheUsageReported"`
	Duration             time.Duration            `json:"duration"`
	Error                string                   `json:"error,omitempty"`
	Budget               providers.BudgetSnapshot `json:"budget"`
}

func (s *Stats) recordStage(name string, st StageStats) {
	s.UnreportedUsageCalls += st.UnreportedUsageCalls
	s.ModelCalls += st.ModelCalls
	s.FailedCalls += st.FailedCalls
	s.InvalidResponses += st.InvalidResponses
	s.PromptTokens += st.PromptTokens
	s.CompletionTokens += st.CompletionTokens
	s.CostUSD += st.CostUSD
	s.CachedTokens += st.CachedTokens
	s.CacheWriteTokens += st.CacheWriteTokens
	s.CacheHits += st.CacheHits
	s.CacheUsageReported = s.CacheUsageReported || st.CacheUsageReported
	if st.ReasoningTokens != nil {
		if s.ReasoningTokens == nil {
			s.ReasoningTokens = new(int)
		}
		*s.ReasoningTokens += *st.ReasoningTokens
	}
	s.CallsByModel[st.Model] += st.ModelCalls
	s.Stages[name] = st
}
