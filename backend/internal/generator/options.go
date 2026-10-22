package generator

import (
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// Document is the input: plain text of the notes and their title.
//
// ID identifies the stored document, such as its set id. Fact ids are
// derived from it and the quote: the same notes uploaded twice must not share
// fact ids, which the storage keeps unique across all sets. Without an ID
// every Generate call picks a random one.
//
// PageStarts are byte offsets in Text where pages 1, 2, … start, as
// ingest.PageInfo.Start gives them: cards then cite pages. Without them, as
// for plain text, cards cite the title only.
type Document struct {
	Structure  *material.Document
	Text       string
	Title      string
	ID         string
	PageStarts []int
}

// Artifact retains private originals and the source representation for auditing.
type Artifact struct {
	Version string            `json:"version"`
	Reading material.Document `json:"reading"`
	Files   []ingest.File     `json:"files"` // private originals for reproducible region references
}

// Result includes cards, available source coverage and processing metrics.
// Plan and Reviews are retained only for decoding historical private reports.
type Result struct {
	Budget providers.BudgetSnapshot `json:"budget"`

	Artifact *Artifact      `json:"artifact,omitempty"`
	Facts    []cards.Fact   `json:"facts"`
	Cards    []cards.Card   `json:"cards"`
	Stats    Stats          `json:"stats"`
	Plan     Plan           `json:"plan,omitempty"`
	Coverage Coverage       `json:"coverage"`
	Reviews  []ReviewRecord `json:"reviews,omitempty"`
}

// Batch is what OnBatch receives: cards now final and the facts first met
// among them. A fact comes once, in the batch with its first card, so the
// storage can insert it before its cards.
type Batch struct {
	Facts []cards.Fact
	Cards []cards.Card
}

// Stats describes one Generate run. Every dropped card and every chunk that
// did not reach the model is counted. Historical fields are retained for saved
// reports; zero counters do not establish semantic correctness.
type Stats struct {
	CacheHits       int                      `json:"cacheHits"`
	CostUSD         float64                  `json:"costUsd"` // provider-reported charges only
	StageTimes      map[string]time.Duration `json:"stageTimes,omitempty"`
	ReviewRejected  int                      `json:"reviewRejected"`
	ReviewFailures  int                      `json:"reviewFailures"`
	CoverageRetries int                      `json:"coverageRetries"`
	Chunks          int                      `json:"chunks"`         // fragments after splitting
	ChunksFiltered  int                      `json:"chunksFiltered"` // junk, never sent to the model
	FilteredBy      map[string]int           `json:"filteredBy"`     // junk reason → fragments
	ChunksSkipped   int                      `json:"chunksSkipped"`  // not sent: MaxFactsPerDoc already used up
	ChunksFailed    int                      `json:"chunksFailed"`   // provider error after its retries and fallback
	ChunksInvalid   int                      `json:"chunksInvalid"`  // no valid JSON after the retry, fragment skipped
	ChunksRefused   int                      `json:"chunksRefused"`  // the content filter of every model blocked it

	ModelCalls       int            `json:"modelCalls"`       // including retries
	InvalidResponses int            `json:"invalidResponses"` // responses that failed JSON/schema parsing
	CallsByModel     map[string]int `json:"callsByModel"`     // model reported by the provider → calls
	PromptTokens     int            `json:"promptTokens"`
	CompletionTokens int            `json:"completionTokens"`
	ReasoningTokens  *int           `json:"reasoningTokens,omitempty"`

	FactsFromModel int `json:"factsFromModel"` // facts in parsed responses
	Facts          int `json:"facts"`          // distinct facts among the cards returned

	// Card counts. CardsFromModel = every Dropped* + Cards.
	CardsFromModel          int `json:"cardsFromModel"`     // cards in parsed responses
	DroppedInvalid          int `json:"droppedInvalid"`     // card breaks the schema: empty field, unknown kind
	DroppedQuote            int `json:"droppedQuote"`       // its fact's quote not found in the fragment
	DroppedPersonal         int `json:"droppedPersonal"`    // its fact's quote holds a password, a phone number or the like
	DroppedUnsupported      int `json:"droppedUnsupported"` // choice or input answer not in the quote, or given in the question; a flip answer its question names; a flip answer or a true claim the quote does not back
	DroppedTable            int `json:"droppedTable"`       // table whose terms the quote does not put in their columns
	DroppedExample          int `json:"droppedExample"`     // asks about names an example invents: «От какого класса наследуется класс B?»
	DroppedDuplicate        int `json:"droppedDuplicate"`   // same fact or same question as earlier
	DroppedByLimit          int `json:"droppedByLimit"`     // its fact is over MaxFactsPerDoc
	DroppedVariants         int `json:"droppedVariants"`    // could not keep its kind, other cards cover the fact
	DowngradedToFlip        int `json:"downgradedToFlip"`   // choice without enough distractors, the fact's only card
	DroppedDistractors      int `json:"droppedDistractors"` // wrong options the quote or the question names: likely right too
	ExplanatoryChoiceToFlip int `json:"explanatoryChoiceToFlip"`
	InputToFlip             int `json:"inputToFlip"` // input with an answer too long to type, the fact's only card
	Cards                   int `json:"cards"`       // cards returned

	// TrimmedExplanations lost a lead-in naming the notes or sentences
	// about the notes or from outside them; the cards stay.
	TrimmedExplanations int `json:"trimmedExplanations"`

	ChunkTimeAvg   time.Duration `json:"chunkTimeAvg"` // model time per fragment, retries included
	ChunkTimeMax   time.Duration `json:"chunkTimeMax"`
	FirstCardAfter time.Duration `json:"firstCardAfter"` // from Generate start to the first OnBatch batch
	Total          time.Duration `json:"total"`
}

// DefaultCallTimeout includes the primary and two fallback providers. The
// fallback divides this deadline between them, leaving five minutes for thinking.
const DefaultCallTimeout = 15 * time.Minute

// Options contains only settings used by the single generation request.
type Options struct {
	Budget         providers.BudgetLimits
	SourceBudget   int // complete source bytes; zero defaults to 80000
	MaxFactsPerDoc int // zero means unlimited
	CallTimeout    time.Duration
	OnBatch        func(Batch)
}

func (o Options) withDefaults() Options {
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
