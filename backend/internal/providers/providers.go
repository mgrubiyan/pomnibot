// Package providers is the common interface to LLM backends. The card
// generator uses it, and so can any other part of the app that needs a model
// (checking free-form answers, for one): prompts and a schema in, raw text
// out. Implementations live in subpackages, such as providers/gigachat.
package providers

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrRefused is returned when the model's content filter blocked the answer.
var ErrRefused = errors.New("refused by the content filter")

// Request is one model call. Schema is a JSON Schema the answer must follow;
// providers pass it to structured output when the API supports it and must
// return the raw text either way: the caller validates the answer itself.
//
// MaxTokens and ReasoningEffort can override provider defaults for staged tasks.
// Empty ReasoningEffort inherits the default; "none" explicitly disables thinking.
type Request struct {
	Source          string
	NoFallback      bool
	PDFs            []PDF
	ReasoningEffort string
	MaxTokens       int
	NoCache         bool // caller must still validate semantics/source links after the response

	System      string
	User        string
	Schema      json.RawMessage
	Temperature float64
	Images      []Image // ordered additional source pages
	Image       *Image  // shown to the model with the user message; nil for text only
}

// PDF is an original source file with a locally verified page count.
type PDF struct {
	StartPage int    `json:"startPage"`
	TextBytes int    `json:"textBytes"`
	Name      string `json:"name"`
	Data      []byte `json:"data"`
	Pages     int    `json:"pages"`
}

// Image is a picture the model reads, such as a photo of a page of notes.
type Image struct {
	Data     []byte
	MimeType string // image/jpeg or image/png
}

// Usage is the token count reported by the provider.
type Usage struct {
	CachedTokens     *int
	CacheWriteTokens *int
	UnreportedCalls  int
	FailedAttempts   int
	Attempts         int
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  *int    // part of CompletionTokens; nil means unreported, not zero
	CostUSD          float64 // billed cost when reported by the provider; zero otherwise
}

// Response is the raw model answer. Content may be wrapped in markdown or
// surrounded by prose: providers do not clean it up.
type Response struct {
	Cached  bool // a reused private processing artifact, no new API billing
	Content []byte
	Usage   Usage
	Model   string // model that actually answered, may be a fallback
	// Truncated: the model ran into the token limit and the answer is cut
	// off.
	Truncated bool
}

// Provider is an LLM backend. Implementations handle auth, rate limits and
// transport retries; the caller handles invalid answers.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// RequestImages returns images in prompt order, preserving the legacy single image.
func RequestImages(r Request) []Image {
	out := make([]Image, 0, len(r.Images)+1)
	if r.Image != nil {
		out = append(out, *r.Image)
	}
	return append(out, r.Images...)
}
