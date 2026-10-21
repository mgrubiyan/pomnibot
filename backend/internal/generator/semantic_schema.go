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
	// Stable schema bytes matter for processing cache invalidation.
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func schemaArray(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
func schemaBytes(v any) json.RawMessage   { b, _ := json.Marshal(v); return b }
func evidenceSchema() map[string]any {
	s := map[string]any{"type": "string"}
	n := map[string]any{"type": "integer", "minimum": 0}
	return schemaObject(map[string]any{"blockId": s, "kind": map[string]any{"type": "string", "enum": []string{"text", "cell", "image"}}, "quote": s, "row": n, "column": n, "observation": s, "region": schemaArray(map[string]any{"type": "number"})})
}
func planSchema() json.RawMessage {
	s := map[string]any{"type": "string", "minLength": 1}
	refs := schemaArray(s)
	refs["minItems"] = 1
	objective := schemaObject(map[string]any{"id": s, "text": s, "blockIds": refs, "priority": map[string]any{"type": "string", "enum": []string{"core", "supporting", "detail"}}, "modes": schemaArray(map[string]any{"type": "string", "enum": []string{"recall", "distinguish", "explain", "apply"}}), "scope": schemaArray(map[string]any{"type": "string"})})
	goals := schemaArray(objective)
	goals["minItems"] = 1
	topic := schemaObject(map[string]any{"id": s, "title": s, "section": s, "blockIds": refs, "objectives": goals})
	return schemaBytes(schemaObject(map[string]any{"topics": schemaArray(topic)}))
}
func reviewSchema() json.RawMessage {
	s := map[string]any{"type": "string"}
	v := schemaObject(map[string]any{"index": map[string]any{"type": "integer", "minimum": 0}, "accept": map[string]any{"type": "boolean"}, "problem": s, "evidence": schemaArray(evidenceSchema()), "covered": schemaArray(s)})
	return schemaBytes(schemaObject(map[string]any{"verdicts": schemaArray(v)}))
}
func topicSchema(maxFacts int) json.RawMessage {
	var schema map[string]any
	_ = json.Unmarshal(cardSchema(maxFacts), &schema)
	fact := schema["properties"].(map[string]any)["facts"].(map[string]any)["items"].(map[string]any)
	fact["properties"].(map[string]any)["cards"].(map[string]any)["maxItems"] = 1
	card := fact["properties"].(map[string]any)["cards"].(map[string]any)["items"].(map[string]any)
	card["properties"].(map[string]any)["learningMode"] = map[string]any{"type": "string", "enum": []string{"recall", "distinguish", "explain", "apply"}}
	card["required"] = append(card["required"].([]any), "learningMode")
	fact["properties"].(map[string]any)["evidence"] = schemaArray(evidenceSchema())
	required := fact["required"].([]any)
	fact["required"] = append(required, "evidence")
	return schemaBytes(schema)
}
