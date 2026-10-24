package generator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// diagnosticError retains private causes for errors.Is/As, but only renders
// reasons and paths constructed by this package. Never render the cause: provider
// and JSON errors can contain document text or arbitrary response fields.
type diagnosticError struct {
	cause           error
	stage           string
	category        string
	reason          string
	path            string
	jsonOffset      int64
	providerStatus  int
	model           string
	truncated       *bool
	responseBytes   *int
	maxTokens       int
	reasoningEffort string
	usage           *providers.Usage
	duration        *time.Duration
}

var _ slog.LogValuer = (*diagnosticError)(nil)

func (e *diagnosticError) Error() string {
	message := e.reason
	if e.path != "" {
		message += " at " + e.path
	}
	if e.jsonOffset > 0 {
		message += fmt.Sprintf(" (JSON byte %d)", e.jsonOffset)
	}
	if e.stage != "" {
		prefix := "generation"
		if e.stage != "generation" {
			prefix += " " + e.stage
		}
		message = prefix + ": " + message
	}
	return message
}

func (e *diagnosticError) Unwrap() error { return e.cause }

func problem(category, path, reason string) *diagnosticError {
	return &diagnosticError{category: category, path: path, reason: reason}
}

func atPath(prefix string, err error) error {
	var detail *diagnosticError
	if !errors.As(err, &detail) {
		// Defensive fallback; a raw parser error must never become a log message.
		return &diagnosticError{category: "invalid_structure", path: prefix, reason: "invalid response structure", cause: err}
	}
	cp := *detail
	cp.cause = err
	cp.path = fieldPath(prefix, detail.path)
	return &cp
}

func fieldPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	if field == "" {
		return prefix
	}
	return prefix + "." + field
}

func providerProblem(err error) *diagnosticError {
	category, reason := "provider", "provider request failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		category, reason = "timeout", "provider request timed out"
	case errors.Is(err, context.Canceled):
		category, reason = "canceled", "provider request was canceled"
	case errors.Is(err, providers.ErrBudget):
		category, reason = "budget", "document provider budget exhausted"
	case errors.Is(err, providers.ErrRefused):
		category, reason = "refused", "provider refused generation"
	case errors.Is(err, providers.ErrCapability):
		category, reason = "capability", "provider does not support generation request"
	case errors.Is(err, providers.ErrBusy):
		category, reason = "busy", "provider is busy"
	case errors.Is(err, providers.ErrUnavailable):
		category, reason = "unavailable", "provider is unavailable"
	}
	detail := &diagnosticError{category: category, reason: reason, cause: err}
	var status interface{ StatusCode() int }
	if errors.As(err, &status) {
		detail.providerStatus = status.StatusCode()
		if detail.providerStatus > 0 {
			detail.reason += fmt.Sprintf(" (HTTP status %d)", detail.providerStatus)
		}
	}
	return detail
}

func contextualProblem(err error, stage string, opts Options, duration time.Duration) *diagnosticError {
	var detail *diagnosticError
	if !errors.As(err, &detail) {
		detail = providerProblem(err)
	}
	cp := *detail
	cp.cause = err
	cp.stage, cp.maxTokens, cp.reasoningEffort = stage, opts.MaxTokens, opts.ReasoningEffort
	cp.duration = &duration
	return &cp
}

// Diagnostics returns safe, structured generation details through error wrappers.
// Missing observations are omitted rather than inferred from aggregate counters.
func Diagnostics(err error) slog.Value {
	var detail *diagnosticError
	if !errors.As(err, &detail) {
		return slog.GroupValue()
	}
	return detail.LogValue()
}

func (e *diagnosticError) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("category", e.category), slog.String("reason", e.reason)}
	if e.stage != "" {
		attrs = append(attrs, slog.String("stage", e.stage))
	}
	if e.path != "" {
		attrs = append(attrs, slog.String("path", e.path))
	}
	if e.jsonOffset > 0 {
		attrs = append(attrs, slog.Int64("json_offset", e.jsonOffset))
	}
	if e.providerStatus > 0 {
		attrs = append(attrs, slog.Int("provider_status", e.providerStatus))
	}
	if e.model != "" {
		attrs = append(attrs, slog.String("model", e.model))
	}
	if e.truncated != nil {
		attrs = append(attrs, slog.Bool("truncated", *e.truncated))
	}
	if e.responseBytes != nil {
		attrs = append(attrs, slog.Int("response_bytes", *e.responseBytes))
	}
	if e.maxTokens > 0 {
		attrs = append(attrs, slog.Int("max_tokens", e.maxTokens))
	}
	// Only known enum values are safe even when Options is supplied directly.
	switch e.reasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
		attrs = append(attrs, slog.String("reasoning_effort", e.reasoningEffort))
	}
	if e.duration != nil {
		attrs = append(attrs, slog.Duration("duration", *e.duration))
	}
	if u := e.usage; u != nil {
		if u.Attempts > 0 {
			attrs = append(attrs, slog.Int("attempts", u.Attempts))
		}
		if u.FailedAttempts > 0 {
			attrs = append(attrs, slog.Int("failed_attempts", u.FailedAttempts))
		}
		// Providers expose no presence bit for ordinary token counts. A positive
		// count is evidence; zero alone may mean unreported, even on success.
		if u.PromptTokens > 0 {
			attrs = append(attrs, slog.Int("prompt_tokens", u.PromptTokens))
		}
		if u.CompletionTokens > 0 {
			attrs = append(attrs, slog.Int("completion_tokens", u.CompletionTokens))
		}
		if u.ReasoningTokens != nil {
			attrs = append(attrs, slog.Int("reasoning_tokens", *u.ReasoningTokens))
		}
		if u.CachedTokens != nil {
			attrs = append(attrs, slog.Int("cached_tokens", *u.CachedTokens))
		}
		if u.CacheWriteTokens != nil {
			attrs = append(attrs, slog.Int("cache_write_tokens", *u.CacheWriteTokens))
		}
		if u.CostUSD > 0 {
			attrs = append(attrs, slog.Float64("cost_usd", u.CostUSD))
		}
		if u.UnreportedCalls > 0 {
			attrs = append(attrs, slog.Int("unreported_usage_calls", u.UnreportedCalls))
		}
	}
	return slog.GroupValue(attrs...)
}
