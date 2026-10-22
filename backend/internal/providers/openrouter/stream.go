package openrouter

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"
)

var (
	errResponseSize   = errors.New("openrouter: response exceeds size limit")
	errStreamProtocol = errors.New("openrouter: invalid or incomplete stream")
	errResponseJSON   = errors.New("openrouter: invalid response JSON")
)

type streamProgress struct {
	Bytes      int64
	Events     int
	Complete   bool
	FirstToken time.Duration
}

// readStream assembles only a complete SSE response. Comments are keepalives,
// not model tokens; terminal usage frames may repeat the finish reason.
func readStream(body io.Reader, began time.Time, log *slog.Logger) (response chatResponse, progress streamProgress, err error) {
	limited := &io.LimitedReader{R: body, N: maxBodyBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), maxBodyBytes+1)
	defer func() { progress.Bytes = int64(maxBodyBytes+1) - limited.N }()
	var event, content strings.Builder
	finish := ""
	var refusal *string
	for scanner.Scan() {
		if limited.N <= 0 {
			return response, progress, errResponseSize
		}
		line := scanner.Text()
		if line != "" {
			field, value, _ := strings.Cut(line, ":")
			if field == "data" {
				if event.Len() > 0 {
					event.WriteByte('\n')
				}
				event.WriteString(strings.TrimPrefix(value, " "))
			}
			continue
		}
		if event.Len() == 0 {
			continue
		}
		data := event.String()
		event.Reset()
		if data == "[DONE]" {
			if finish == "" {
				return response, progress, errStreamProtocol
			}
			text := content.String()
			response.Choices = []responseChoice{{Message: responseMessage{Content: &text, Refusal: refusal}, FinishReason: finish}}
			progress.Complete = true
			return response, progress, nil
		}
		var chunk chatResponse
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return response, progress, errStreamProtocol
		}
		progress.Events++
		if chunk.ID != "" {
			response.ID = chunk.ID
		}
		if chunk.Model != "" {
			response.Model = chunk.Model
		}
		if chunk.Provider != "" {
			response.Provider = chunk.Provider
		}
		if chunk.Usage.Cost != nil {
			response.Usage.Cost = chunk.Usage.Cost
		}
		if chunk.Usage.PromptTokens != 0 {
			response.Usage.PromptTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionDetails.ReasoningTokens != nil {
			response.Usage.CompletionDetails.ReasoningTokens = chunk.Usage.CompletionDetails.ReasoningTokens
		}
		if chunk.Usage.CompletionTokens != 0 {
			response.Usage.CompletionTokens = chunk.Usage.CompletionTokens
		}
		if chunk.Error != nil {
			return response, progress, &APIError{Status: chunk.Error.status()}
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				return response, progress, errStreamProtocol
			}
			if choice.Error != nil {
				return response, progress, &APIError{Status: choice.Error.status()}
			}
			if choice.FinishReason == "error" {
				return response, progress, &APIError{Status: 502}
			}
			if choice.Delta.Refusal != nil {
				refusal = choice.Delta.Refusal
			}
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				if finish != "" {
					return response, progress, errStreamProtocol
				}
				if progress.FirstToken == 0 {
					progress.FirstToken = time.Since(began)
					log.Info("openrouter: first token", "first_token_after_ms", float64(progress.FirstToken)/float64(time.Millisecond))
				}
				content.WriteString(*choice.Delta.Content)
			}
			if choice.FinishReason != "" {
				if finish != "" && finish != choice.FinishReason {
					return response, progress, errStreamProtocol
				}
				finish = choice.FinishReason
			}
		}
	}
	if limited.N <= 0 {
		return response, progress, errResponseSize
	}
	if err = scanner.Err(); err != nil {
		return response, progress, err
	}
	return response, progress, io.ErrUnexpectedEOF
}
