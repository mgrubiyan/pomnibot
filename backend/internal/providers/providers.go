// Package providers is the common interface to LLM backends. The card
// generator uses it, and so can any other part of the app that needs a model
// (checking free-form answers, for one): prompts and a schema in, raw text
// out. Implementations live in subpackages, such as providers/gigachat.
package providers

import (
	"context"
	"encoding/json"
)

// Request is one model call. Schema is a JSON Schema the answer must follow;
// providers pass it to structured output when the API supports it and must
// return the raw text either way: the caller validates the answer itself.
//
// The completion budget (max tokens) is not here: it depends on the model's
// tokenizer and price, so each provider sets it in its own config.
type Request struct {
	System      string
	User        string
	Schema      json.RawMessage
	Temperature float64
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

// Provider is an LLM backend. Implementations handle auth, rate limits and
// transport retries; the caller handles invalid answers.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
