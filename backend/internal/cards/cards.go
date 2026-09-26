// Package cards turns lecture notes into self-check cards.
//
// The pipeline never trusts the model with facts: every card must carry a
// verbatim quote from the fragment it was generated from, and the quote is
// checked in code. Wrong options for choice cards are taken from answers to
// other fragments of the same document, never invented. See docs/concept.md,
// section 6.
//
// The package is storage-agnostic: text in, cards and stats out. The model is
// hidden behind Provider, so GigaChat, YandexGPT or any OpenAI-compatible API
// plug in without changes here.
package cards

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Kind is the card type. Mirrors CardKind in miniapp/src/types.ts, without
// table cards: those need a layout the model cannot ground in one quote.
type Kind string

// Card kinds.
const (
	KindChoice  Kind = "choice"
	KindBoolean Kind = "boolean"
	KindFlip    Kind = "flip"
	KindInput   Kind = "input"
)

// Boolean answers, as the frontend expects them.
const (
	AnswerTrue  = "true"
	AnswerFalse = "false"
)

// Card mirrors Card in miniapp/src/types.ts without id and setId, which the
// storage assigns, plus FactID.
//
// Cards with the same FactID test one fact in different forms (choice, input,
// boolean, flip) and share the quote. The feed is meant to show one of them
// at a time and rotate the form between repetitions.
type Card struct {
	FactID      string   `json:"factId"` // stable within a document: derived from the quote
	Kind        Kind     `json:"kind"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"` // choice only, includes Answer
	Answer      string   `json:"answer"`            // "true" | "false" for boolean
	Explanation string   `json:"explanation"`
	SourceQuote string   `json:"sourceQuote"` // exact text of the normalized notes
	SourceRef   string   `json:"sourceRef,omitempty"`
	Topic       string   `json:"topic"`
}

// Document is the input: plain text of the notes and their title.
type Document struct {
	Text  string
	Title string
}

// Result is what Generate returns: accepted cards in document order and stats.
type Result struct {
	Cards []Card `json:"cards"`
	Stats Stats  `json:"stats"`
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
	CardsFromModel   int `json:"cardsFromModel"`   // cards in parsed responses
	DroppedInvalid   int `json:"droppedInvalid"`   // card breaks the schema: empty field, unknown kind
	DroppedQuote     int `json:"droppedQuote"`     // its fact's quote not found in the fragment
	DroppedDuplicate int `json:"droppedDuplicate"` // same fact or same question as earlier
	DroppedByLimit   int `json:"droppedByLimit"`   // its fact is over MaxFactsPerDoc
	DroppedVariants  int `json:"droppedVariants"`  // could not keep its kind, other cards cover the fact
	DowngradedToFlip int `json:"downgradedToFlip"` // choice without enough distractors, the fact's only card
	InputToFlip      int `json:"inputToFlip"`      // input with an answer too long to type, the fact's only card
	Cards            int `json:"cards"`            // cards returned

	ChunkTimeAvg   time.Duration `json:"chunkTimeAvg"` // model time per fragment, retries included
	ChunkTimeMax   time.Duration `json:"chunkTimeMax"`
	FirstCardAfter time.Duration `json:"firstCardAfter"` // from Generate start to the first OnCards batch
	Total          time.Duration `json:"total"`
}

// Request is one model call. Schema is a JSON Schema the answer must follow;
// providers pass it to structured output when the API supports it and must
// return the raw text either way: the caller validates the answer itself.
type Request struct {
	System      string
	User        string
	Schema      json.RawMessage
	Temperature float64
	MaxTokens   int
}

// Usage is the token count reported by the provider.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Response is the raw model answer. Content may be wrapped in markdown or
// surrounded by prose: providers do not clean it up.
type Response struct {
	Content []byte
	Usage   Usage
	Model   string // model that actually answered, may be a fallback
}

// Provider is an LLM backend. It knows nothing about cards: prompts and schema
// in, raw bytes and usage out. Implementations handle auth, rate limits and
// transport retries; the caller handles invalid answers.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
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
	OnCards func([]Card)
	Logger  *slog.Logger
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
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}
