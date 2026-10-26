package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

// Exercise the real HTTP adapter: checking only a fake Provider would miss a
// dropped schema or outdated instructions in the request sent to OpenRouter.
func TestOpenRouterGenerationSendsRecallContractAndExpandsPairs(t *testing.T) {
	for _, pdf := range []bool{false, true} {
		name := "text"
		if pdf {
			name = "PDF"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			raw := recallReply(`{"kind":"recall","question":"Other aspect?","answer":"B","source":"S","sourceQuote":""}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if pdf {
					call--
				}
				var body struct {
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					ResponseFormat struct {
						Type       string `json:"type"`
						JSONSchema struct {
							Strict bool            `json:"strict"`
							Schema json.RawMessage `json:"schema"`
						} `json:"json_schema"`
					} `json:"response_format"`
					Provider struct {
						RequireParameters bool  `json:"require_parameters"`
						AllowFallbacks    *bool `json:"allow_fallbacks"`
					} `json:"provider"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if body.ResponseFormat.Type != "json_schema" || !body.ResponseFormat.JSONSchema.Strict || !body.Provider.RequireParameters || body.Provider.AllowFallbacks == nil || *body.Provider.AllowFallbacks {
					t.Error("missing strict schema or provider constraints")
				}
				if pdf && call == 0 {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"model": openrouter.DefaultModel, "choices": []any{map[string]any{"message": map[string]any{"content": `{"figures":[]}`}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "cost": 0}})
					return
				}
				if call == 2 {
					if len(body.Messages) != 2 || !strings.Contains(string(body.ResponseFormat.JSONSchema.Schema), `"illustrations"`) {
						t.Error("illustration stage must omit the document and send its own schema")
					}
					var system string
					_ = json.Unmarshal(body.Messages[0].Content, &system)
					if system != prompts.GeneratorIllustrations {
						t.Error("missing illustration prompt")
					}
					var payload string
					_ = json.Unmarshal(body.Messages[1].Content, &payload)
					var input struct {
						Cards []struct {
							ID string `json:"id"`
						} `json:"cards"`
					}
					if err := json.Unmarshal([]byte(payload), &input); err != nil || len(input.Cards) != 2 {
						t.Error("lost recall bindings")
					}
					content, _ := json.Marshal(map[string]any{"illustrations": []any{map[string]any{"cardId": "f0-c0", "questionSvg": testSVG, "explanationSvg": ""}}})
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"model": openrouter.DefaultModel, "choices": []any{map[string]any{"message": map[string]any{"content": string(content)}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "cost": 0}})
					return
				}
				var gotSchema, wantSchema any
				if err := json.Unmarshal(body.ResponseFormat.JSONSchema.Schema, &gotSchema); err != nil {
					t.Error(err)
				}
				if err := json.Unmarshal(stageSchema(), &wantSchema); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(gotSchema, wantSchema) {
					t.Error("HTTP request does not carry the current recall schema")
				}
				if len(body.Messages) != 3 {
					t.Errorf("messages=%d", len(body.Messages))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var system, taskText string
				if err := json.Unmarshal(body.Messages[0].Content, &system); err != nil {
					t.Error(err)
				}
				if system != prompts.GeneratorSystem || body.Messages[0].Role != "system" {
					t.Error("HTTP request does not carry the current system prompt")
				}
				if err := json.Unmarshal(body.Messages[2].Content, &taskText); err != nil {
					t.Error(err)
				}
				var task struct {
					Task string `json:"task"`
				}
				if err := json.Unmarshal([]byte(taskText), &task); err != nil {
					t.Error(err)
				}
				if task.Task != prompts.GenerateGoals {
					t.Error("HTTP request does not carry the current generation prompt")
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"model": openrouter.DefaultModel, "choices": []any{map[string]any{"message": map[string]any{"content": raw}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "cost": 0}}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := openrouter.New(openrouter.Config{APIKey: "test-only", Model: openrouter.DefaultModel, BaseURL: server.URL, MaxTokens: 8000, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			doc := Document{Text: "source"}
			if pdf {
				doc = pdfDocument()
			}
			published := 0
			result, err := NewGenerator(client, Options{OnBatch: func(Batch) { published++ }}).Generate(t.Context(), doc)
			if err != nil {
				t.Fatal(err)
			}
			expectedCalls := int32(2)
			if pdf {
				expectedCalls++
			}
			if calls.Load() != expectedCalls || published != 1 || result.Stats.Cards != 4 || result.Stats.CardsFromModel != 0 || result.Stats.CardsSynthesized != 4 || result.Facts[0].Cards[0].Base().QuestionSvg != testSVG || result.Stats.FailedCalls != 0 {
				t.Fatalf("calls=%d published=%d stats=%+v", calls.Load(), published, result.Stats)
			}
			for i := range 2 {
				flip, input := result.Facts[0].Cards[i].(FlipCard), result.Facts[0].Cards[2+i].(InputCard)
				if !reflect.DeepEqual(flip.CardBase, input.CardBase) || flip.Answer != input.Answer {
					t.Fatal("expanded pair differs")
				}
			}
		})
	}
}
