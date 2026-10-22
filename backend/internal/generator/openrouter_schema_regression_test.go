package generator

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestDirectSchemaCompatibleWithOpenRouterClaudeStructuredOutputs(t *testing.T) {
	// Reproduced against anthropic/claude-haiku-5.5 on 2026-10-22:
	// HTTP 400: output_config.format.schema: Schema type 'oneOf' is not supported.
	// Claude also rejects string/numeric bounds and array constraints other than
	// minItems 0 or 1. Keep those checks in the local response validators instead.
	// https://platform.claude.com/docs/en/build-with-claude/structured-outputs#json-schema-limitations
	// The request and provider error are retained in
	// artifacts/cardsgen-audit-20261022/attempt-2/. This guard is entirely offline.
	var schema any
	if err := json.Unmarshal(directSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	var check func(any, string)
	check = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			if value["type"] == "object" && value["additionalProperties"] != false {
				t.Errorf("%s: objects must disallow additional properties", path)
			}
			for key, child := range value {
				childPath := path + "." + key
				switch key {
				case "oneOf", "contains", "uniqueItems", "minLength", "maxLength", "minimum", "maximum", "multipleOf", "maxItems":
					t.Errorf("%s: unsupported Claude structured-output constraint", childPath)
				case "minItems":
					if child != float64(0) && child != float64(1) {
						t.Errorf("%s: only 0 or 1 is supported, got %v", childPath, child)
					}
				}
				check(child, childPath)
			}
		case []any:
			for i, child := range value {
				check(child, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	check(schema, "$")
}

func TestStoredCardEnforcesConstraintsOmittedFromProviderSchema(t *testing.T) {
	q := CardBase{Question: "Q"}
	for _, tc := range []struct {
		name string
		card Card
	}{
		{"blank question", FlipCard{CardBase: CardBase{Question: " "}, Answer: "A"}},
		{"blank answer", InputCard{CardBase: q, Answer: " "}},
		{"no correct choice", ChoiceCard{CardBase: q, WrongAnswers: []string{"B"}}},
		{"no incorrect choice", ChoiceCard{CardBase: q, RightAnswers: []string{"A"}}},
		{"duplicate choice", ChoiceCard{CardBase: q, RightAnswers: []string{"A"}, WrongAnswers: []string{" a "}}},
		{"one table category", TableCard{CardBase: q, Columns: []Column{{Name: "X", Rows: []string{"A"}}}}},
		{"empty table category", TableCard{CardBase: q, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "Y"}}}},
		{"duplicate table row", TableCard{CardBase: q, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "Y", Rows: []string{"A"}}}}},
		{"duplicate table category", TableCard{CardBase: q, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "X", Rows: []string{"B"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := storedCard(tc.card); ok {
				t.Fatal("accepted invalid learning content")
			}
		})
	}
}
