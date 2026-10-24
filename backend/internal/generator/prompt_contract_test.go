package generator

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
)

func TestPromptsAndSchemaUseNestedFacts(t *testing.T) {
	for name, prompt := range map[string]string{"system": prompts.GeneratorSystem, "task": prompts.GenerateGoals} {
		for _, old := range []string{"goals", "goalId", "topic"} {
			if strings.Contains(prompt, old) {
				t.Errorf("%s contains legacy instruction %q", name, old)
			}
		}
		for _, field := range []string{"facts", "recall", "extraCards", "name", "pages", "source", "sourceQuote", "flip", "input", "issues"} {
			if !strings.Contains(prompt, field) {
				t.Errorf("%s omits %s", name, field)
			}
		}
		if !strings.Contains(prompt, "запрещено ссылаться на номера страниц, слайдов, лекций или документа") {
			t.Errorf("%s must explicitly forbid referencing page or slide numbers in questions and answers", name)
		}
		if !strings.Contains(prompt, "Не смешивай в ответе определение понятия и вычисление конкретного примера") {
			t.Errorf("%s must instruct not to mix definitions and example calculations in answers", name)
		}
		if !strings.Contains(prompt, "в примере со слайда 33") {
			t.Errorf("%s must forbid page/slide references in premises", name)
		}
	}
	if !strings.Contains(prompts.GeneratorSystem, "одинаковыми question, answer, source и sourceQuote") {
		t.Fatal("system prompt must request a matching mandatory pair")
	}
	var root map[string]any
	if err := json.Unmarshal(stageSchema(), &root); err != nil {
		t.Fatal(err)
	}
	object := func(obj map[string]any, fields ...string) map[string]any {
		t.Helper()
		props := obj["properties"].(map[string]any)
		required := obj["required"].([]any)
		if len(props) != len(fields) || len(required) != len(fields) {
			t.Fatalf("unexpected object fields: %+v", obj)
		}
		for _, f := range fields {
			if props[f] == nil || !slices.Contains(required, any(f)) {
				t.Fatalf("missing required %s", f)
			}
		}
		return props
	}
	props := object(root, "facts", "issues")
	fact := object(props["facts"].(map[string]any)["items"].(map[string]any), "name", "pages", "recall", "extraCards")
	object(fact["recall"].(map[string]any), "question", "answer", "source", "sourceQuote")
	variants := fact["extraCards"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(variants) != 4 {
		t.Fatal("missing card formats")
	}
	for i, v := range variants {
		kind := v.(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["const"]
		if kind != []string{"recall", "boolean", "choice", "table"}[i] {
			t.Fatalf("unexpected kind %v", kind)
		}
		fields := []string{"kind", "question", "source", "sourceQuote"}
		switch i {
		case 2:
			fields = append(fields, "rightAnswers", "wrongAnswers")
		case 3:
			fields = append(fields, "columns")
		default:
			fields = append(fields, "answer")
		}
		object(v.(map[string]any), fields...)
	}
}

func TestGenerationPromptsRequestGFMAndLaTeX(t *testing.T) {
	for name, prompt := range map[string]string{"system": prompts.GeneratorSystem, "task": prompts.GenerateGoals} {
		for _, text := range []string{"GFM + math", "$...$", "$$...$$", "Markdown", "sourceQuote"} {
			if !strings.Contains(prompt, text) {
				t.Errorf("%s lacks %q", name, text)
			}
		}
		for _, obsolete := range []string{"не используй LaTeX", "Интерфейс показывает обычный текст", "Формулы оформляй обычным текстом"} {
			if strings.Contains(prompt, obsolete) {
				t.Errorf("%s contains conflicting rule", name)
			}
		}
		for _, line := range strings.Split(prompt, "\n") {
			if !strings.HasPrefix(line, `{"facts":`) {
				continue
			}
			var reply AIResponse
			if err := json.Unmarshal([]byte(line), &reply); err != nil {
				t.Fatalf("invalid example: %v", err)
			}
			answer := reply.Facts[0].Cards[0].(FlipCard).Answer
			if !strings.Contains(answer, `\cdot`) || !strings.Contains(answer, "$") {
				t.Fatalf("example loses LaTeX: %q", answer)
			}
		}
	}
}
