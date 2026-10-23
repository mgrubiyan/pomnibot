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
		for _, old := range []string{"goals", "goalId", "topic", "создаёт из них", "создаст сервер"} {
			if strings.Contains(prompt, old) {
				t.Errorf("%s contains legacy instruction %q", name, old)
			}
		}
		for _, field := range []string{"facts", "cards", "name", "pages", "source", "sourceQuote", "flip", "input", "issues"} {
			if !strings.Contains(prompt, field) {
				t.Errorf("%s omits %s", name, field)
			}
		}
	}
	if !strings.Contains(prompts.GeneratorSystem, "одинаковые question и answer") {
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
	fact := object(props["facts"].(map[string]any)["items"].(map[string]any), "name", "pages", "cards")
	variants := fact["cards"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(variants) != 5 {
		t.Fatal("missing card formats")
	}
	for i, v := range variants {
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
