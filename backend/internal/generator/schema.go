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
