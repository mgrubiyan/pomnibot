package generator

import (
	"encoding/json"
	"slices"
)

func schemaObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for k := range properties {
		required = append(required, k)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func schemaArray(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
func schemaBytes(v any) json.RawMessage   { data, _ := json.Marshal(v); return data }

func tableColumnsSchema(s any) map[string]any {
	a := schemaArray(schemaObject(map[string]any{"name": s, "rows": map[string]any{"type": "array", "items": s, "minItems": 1}}))
	a["minItems"] = 1
	a["description"] = "At least two distinct categories, each with nonempty rows. Every row is unique across all categories."
	return a
}

func factSchema() map[string]any {
	// Use the structured-output subset supported by Claude through OpenRouter.
	// Local DTO and publication validation require both flip and input per fact;
	// storedCard enforces nonempty values, unique answers/rows and table categories.
	s := map[string]any{"type": "string", "description": "Nonempty text."}
	nonempty := func(v any) map[string]any { a := schemaArray(v); a["minItems"] = 1; return a }
	variant := func(kind string, fields map[string]any) any {
		fields["kind"] = map[string]any{"type": "string", "const": kind}
		fields["question"] = s
		return schemaObject(fields)
	}
	variants := []any{
		variant("flip", map[string]any{"answer": s}),
		variant("boolean", map[string]any{"answer": map[string]any{"type": "boolean"}}),
		variant("choice", map[string]any{"rightAnswers": nonempty(s), "wrongAnswers": nonempty(s)}),
		variant("table", map[string]any{"columns": tableColumnsSchema(s)}),
		variant("input", map[string]any{"answer": s}),
	}
	// Distinct kind constants make the variants mutually exclusive under anyOf.
	cs := nonempty(map[string]any{"anyOf": variants})
	cs["description"] = "At least one flip card and one input card are required in every fact."
	return schemaObject(map[string]any{"name": s, "cards": cs})
}
func stageSchema() json.RawMessage {
	s := map[string]any{"type": "string"}
	goal := schemaObject(map[string]any{"goalId": s, "name": s, "topic": s, "pages": schemaArray(map[string]any{"type": "integer"}), "source": s})
	fact := factSchema()
	variants := fact["properties"].(map[string]any)["cards"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	content := schemaObject(map[string]any{"name": s, "flip": variants[0], "input": variants[4], "cards": schemaArray(map[string]any{"anyOf": variants})})
	group := schemaObject(map[string]any{"goalId": s, "content": content})
	return schemaBytes(schemaObject(map[string]any{"issues": schemaArray(s), "complete": map[string]any{"type": "boolean"}, "goals": schemaArray(goal), "cards": schemaArray(group), "additions": schemaArray(goal)}))
}
