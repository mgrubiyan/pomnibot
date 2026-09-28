package generator

import (
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// Document is the input: plain text of the notes and their title.
//
// ID identifies the stored document, such as its set id. Fact ids are
// derived from it and the quote: the same notes uploaded twice must not share
// fact ids, which the storage keeps unique across all sets. Without an ID
// every Generate call picks a random one.
type Document struct {
	Text  string
	Title string
	ID    string
}

// Result is what Generate returns: accepted cards in document order and stats.
type Result struct {
	Cards []cards.Card `json:"cards"`
	Stats Stats        `json:"stats"`
}

// Stats describes one Generate run. Every dropped card and every chunk that
// did not reach the model is counted with its reason: these numbers are how we
// show that the "no made-up facts" filter actually works.
type Stats struct {
	Chunks         int            `json:"chunks"`         // fragments after splitting
	ChunksFiltered int            `json:"chunksFiltered"` // junk, never sent to the model
	FilteredBy     map[string]int `json:"filteredBy"`     // junk reason → fragments
	ChunksSkipped  int            `json:"chunksSkipped"`  // not sent: MaxFactsPerDoc already used up
	ChunksFailed   int            `json:"chunksFailed"`   // provider error after its retries and fallback
	ChunksInvalid  int            `json:"chunksInvalid"`  // no valid JSON after the retry, fragment skipped

	ModelCalls       int            `json:"modelCalls"`       // including retries
	InvalidResponses int            `json:"invalidResponses"` // responses that failed JSON/schema parsing
	CallsByModel     map[string]int `json:"callsByModel"`     // model reported by the provider → calls
	PromptTokens     int            `json:"promptTokens"`
	CompletionTokens int            `json:"completionTokens"`

	FactsFromModel int `json:"factsFromModel"` // facts in parsed responses
	Facts          int `json:"facts"`          // distinct facts among the cards returned

	// Card counts. CardsFromModel = every Dropped* + Cards.
	CardsFromModel     int `json:"cardsFromModel"`     // cards in parsed responses
	DroppedInvalid     int `json:"droppedInvalid"`     // card breaks the schema: empty field, unknown kind
	DroppedQuote       int `json:"droppedQuote"`       // its fact's quote not found in the fragment
	DroppedUnsupported int `json:"droppedUnsupported"` // choice or input answer not in the quote, or given in the question
	DroppedDuplicate   int `json:"droppedDuplicate"`   // same fact or same question as earlier
	DroppedByLimit     int `json:"droppedByLimit"`     // its fact is over MaxFactsPerDoc
	DroppedVariants    int `json:"droppedVariants"`    // could not keep its kind, other cards cover the fact
	DowngradedToFlip   int `json:"downgradedToFlip"`   // choice without enough distractors, the fact's only card
	InputToFlip        int `json:"inputToFlip"`        // input with an answer too long to type, the fact's only card
	Cards              int `json:"cards"`              // cards returned

	ChunkTimeAvg   time.Duration `json:"chunkTimeAvg"` // model time per fragment, retries included
	ChunkTimeMax   time.Duration `json:"chunkTimeMax"`
	FirstCardAfter time.Duration `json:"firstCardAfter"` // from Generate start to the first OnCards batch
	Total          time.Duration `json:"total"`
}

// Defaults for Options.
const (
	// DefaultConcurrency is 1 because the GigaChat freemium plan allows exactly
	// one concurrent request; a second one gets 429.
	DefaultConcurrency      = 1
	DefaultChunkSize        = 2500
	DefaultChunkOverlap     = 150
	DefaultMaxFactsPerChunk = 3
	DefaultCallTimeout      = 2 * time.Minute
)

// Options configure a Generator. Zero values mean defaults.
type Options struct {
	Concurrency      int           // parallel model calls, default 1
	ChunkSize        int           // fragment size in characters, default 2500
	ChunkOverlap     int           // characters shared with the previous fragment, default 150, negative for none
	MaxFactsPerChunk int           // facts per fragment, default 3; each fact gets up to 4 cards
	MaxFactsPerDoc   int           // 0 means no limit
	CallTimeout      time.Duration // per model call, retries of the provider included
	// OnCards receives cards as soon as they are final, so a long document
	// shows progress in minutes rather than all at once at the end. It is
	// called from a single goroutine, outside locks, only with non-empty
	// batches; a panic in it is logged and does not stop generation.
	OnCards func([]cards.Card)
}

func (o Options) withDefaults() Options {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	if o.ChunkSize <= 0 {
		o.ChunkSize = DefaultChunkSize
	}
	switch {
	case o.ChunkOverlap == 0:
		o.ChunkOverlap = DefaultChunkOverlap
	case o.ChunkOverlap < 0:
		o.ChunkOverlap = 0
	}
	// Overlap is context, not content: a large one means paying for the
	// same text twice.
	o.ChunkOverlap = min(o.ChunkOverlap, o.ChunkSize/4)
	if o.MaxFactsPerChunk <= 0 {
		o.MaxFactsPerChunk = DefaultMaxFactsPerChunk
	}
	if o.MaxFactsPerDoc < 0 {
		o.MaxFactsPerDoc = 0
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = DefaultCallTimeout
	}
	return o
}
