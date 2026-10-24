package generator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// diagnosticError retains private causes for errors.Is/As, but only renders
// reasons and paths constructed by this package. Never render the cause: provider
// and JSON errors can contain document text or arbitrary response fields.
type diagnosticError struct {
	cause                         error
	stage, category, reason, path string
	attrs                         []slog.Attr
	jsonOffset                    int64
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
		if code := status.StatusCode(); code > 0 {
			detail.reason += fmt.Sprintf(" (HTTP status %d)", code)
			detail.attrs = append(detail.attrs, slog.Int("provider_status", code))
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
	cp.stage = stage
	cp.attrs = append(slices.Clone(detail.attrs), slog.Duration("duration", duration))
	if opts.MaxTokens > 0 {
		cp.attrs = append(cp.attrs, slog.Int("max_tokens", opts.MaxTokens))
	}
	switch opts.ReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
		cp.attrs = append(cp.attrs, slog.String("reasoning_effort", opts.ReasoningEffort))
	}
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
	return slog.GroupValue(append(attrs, e.attrs...)...)
}

func (e *diagnosticError) addResponse(resp providers.Response, observed bool) {
	if resp.Model != "" {
		e.attrs = append(e.attrs, slog.String("model", resp.Model))
	}
	if observed {
		e.attrs = append(e.attrs, slog.Bool("truncated", resp.Truncated), slog.Int("response_bytes", len(resp.Content)))
	}
	u := resp.Usage
	// These provider counters have no presence bit: zero can mean unreported.
	for _, counter := range []struct {
		key   string
		value int
	}{
		{"attempts", u.Attempts}, {"failed_attempts", u.FailedAttempts},
		{"prompt_tokens", u.PromptTokens}, {"completion_tokens", u.CompletionTokens},
		{"unreported_usage_calls", u.UnreportedCalls},
	} {
		if counter.value > 0 {
			e.attrs = append(e.attrs, slog.Int(counter.key, counter.value))
		}
	}
	for _, counter := range []struct {
		key   string
		value *int
	}{
		{"reasoning_tokens", u.ReasoningTokens}, {"cached_tokens", u.CachedTokens}, {"cache_write_tokens", u.CacheWriteTokens},
	} {
		if counter.value != nil {
			e.attrs = append(e.attrs, slog.Int(counter.key, *counter.value))
		}
	}
	if u.CostUSD > 0 {
		e.attrs = append(e.attrs, slog.Float64("cost_usd", u.CostUSD))
	}
}
