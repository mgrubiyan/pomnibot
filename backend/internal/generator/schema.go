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
	// Each recall expands into flip/input during local publication validation;
	// validateCard enforces nonempty values, unique answers/rows and table categories.
	s := map[string]any{"type": "string", "description": "Nonempty GFM + math text. Inline LaTeX: $...$; display LaTeX: $$...$$. JSON-escape backslashes."}
	nonempty := func(v any) map[string]any { a := schemaArray(v); a["minItems"] = 1; return a }
	variant := func(kind string, fields map[string]any) any {
		fields["kind"] = map[string]any{"type": "string", "const": kind}
		fields["question"] = s
		fields["source"] = map[string]any{"type": "string", "description": "Nonempty GFM + math explanation supporting this card, including LaTeX formulas and conditions."}
		fields["sourceQuote"] = map[string]any{"type": "string", "description": "Verbatim quote for this card from the supplied text or readable original PDF on the fact pages; empty string if unavailable or not confidently readable."}
		return schemaObject(fields)
	}
	recall := variant("recall", map[string]any{"answer": s}).(map[string]any)
	props := recall["properties"].(map[string]any)
	delete(props, "kind")
	recall = schemaObject(props)
	variants := []any{
		variant("boolean", map[string]any{"answer": map[string]any{"type": "boolean"}}),
		variant("choice", map[string]any{"rightAnswers": nonempty(s), "wrongAnswers": nonempty(s)}),
		variant("table", map[string]any{"columns": tableColumnsSchema(s)}),
	}
	// Distinct kind constants make the variants mutually exclusive under anyOf.
	cs := schemaArray(map[string]any{"anyOf": variants})
	cs["description"] = "Optional additional boolean, choice and table cards; may be empty."
	pages := nonempty(map[string]any{"type": "integer"})
	pages["description"] = "Source pages for this fact, numbered from 1 across all files."
	return schemaObject(map[string]any{"name": map[string]any{"type": "string", "description": "Nonempty plain-text fact name, without Markdown or math."}, "pages": pages, "recall": recall, "extraCards": cs})
}
func stageSchema() json.RawMessage {
	facts := schemaArray(factSchema())
	facts["minItems"] = 1
	return schemaBytes(schemaObject(map[string]any{"issues": schemaArray(map[string]any{"type": "string"}), "facts": facts}))
}
