package notes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestMakeLogsSelfContainedSafeGenerationFailure(t *testing.T) {
	const private = "PRIVATE_DOCUMENT_MODEL_TEXT"
	content := `{"issues":[],"facts":[{"name":"` + private + `","pages":[1],"cards":[{"kind":"input","question":"` + private + `","source":"` + private + `","sourceQuote":"` + private + `","answer":"` + private + `"}]}]}`
	for _, tc := range []struct {
		name, content, category, path, reason, userText string
		cause                                           error
	}{
		{"required flip", content, "validation", "facts[0].cards", "missing required flip card", TextTryLater, nil},
		{"empty answer", strings.Replace(content, `}]}]}`, `},{"kind":"flip","question":"Q","answer":" ","source":"S","sourceQuote":""}]}]}`, 1), "validation", "facts[0].cards[1].answer", "answer is empty", TextTryLater, nil},
		{"truncated", content, "truncated", "", "response was truncated", TextTryLater, nil},
		{"unknown key", `{"issues":[],"facts":[],"` + private + `":1}`, "invalid_structure", "<unknown>", "unknown field", TextTryLater, nil},
		{"broken JSON", `{"` + private + `":!}`, "invalid_json", "", "invalid JSON", TextTryLater, nil},
		{"provider refusal", "", "refused", "", "provider refused generation", TextOtherText, providers.ErrRefused},
		{"budget", "", "budget", "", "document provider budget exhausted", TextBudgetExceeded, providers.ErrBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			model := directProvider(func(context.Context, providers.Request) (providers.Response, error) {
				var err error
				if tc.cause != nil {
					err = fmt.Errorf("%s: %w", private, tc.cause)
				}
				return providers.Response{Content: []byte(tc.content), Truncated: tc.name == "truncated", Model: "test-model", Usage: providers.Usage{Attempts: 2, FailedAttempts: 1, PromptTokens: 123, CompletionTokens: 45, ReasoningTokens: new(7), CostUSD: .02}}, err
			})
			ex := &fakeExtractor{result: ingest.Result{Text: private}}
			saver := &captureDirectSaver{}
			userID := uuid.New()
			_, err := New(ex, FromGenerator(generator.NewGenerator(model, generator.Options{})), saver, 1).Make(t.Context(), Request{UserID: userID, Caption: "Lecture", Files: file("notes.txt")}, Progress{})
			if err == nil || saver.calls != 0 {
				t.Fatal("expected failed generation without save")
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("lost cause: %v", err)
			}
			if got := FailureText(err); got != tc.userText {
				t.Errorf("user text changed: %s", got)
			}
			if strings.Contains(logs.String(), private) || strings.Contains(err.Error(), private) {
				t.Fatalf("private text in logs or error: %s, %v", &logs, err)
			}
			var failure map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var record map[string]any
				if e := json.Unmarshal(line, &record); e != nil {
					t.Fatal(e)
				}
				if record["msg"] == "failed to generate cards from document" {
					failure = record
				}
			}
			if failure == nil {
				t.Fatal("missing final failure record")
			}
			if failure["user_id"] != userID.String() || failure["title"] != "Lecture" || failure["error"] == nil {
				t.Fatalf("lost existing attributes: %v", failure)
			}
			message, ok := failure["error"].(string)
			if !ok || !strings.Contains(message, tc.reason) {
				t.Errorf("error must retain a safe message: %v", failure["error"])
			}
			diagnostic, ok := failure["generation"].(map[string]any)
			if !ok {
				t.Fatalf("missing structured diagnostic: %v", failure)
			}
			for key, want := range map[string]any{"stage": "generation", "category": tc.category, "reason": tc.reason, "model": "test-model", "max_tokens": float64(128000), "reasoning_effort": "medium", "attempts": float64(2), "prompt_tokens": float64(123), "completion_tokens": float64(45), "reasoning_tokens": float64(7), "cost_usd": .02} {
				if diagnostic[key] != want {
					t.Errorf("%s = %v, want %v", key, diagnostic[key], want)
				}
			}
			if tc.path != "" && diagnostic["path"] != tc.path {
				t.Errorf("path = %v, want %s", diagnostic["path"], tc.path)
			}
			if diagnostic["duration"] == nil {
				t.Error("missing stage duration")
			}
			if tc.cause == nil && (diagnostic["response_bytes"] != float64(len(tc.content)) || diagnostic["truncated"] != (tc.name == "truncated")) {
				t.Errorf("missing response metadata: %v", diagnostic)
			}
		})
	}
}
