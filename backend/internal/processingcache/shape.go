package processingcache

import (
	"encoding/json"
	"math"
	"reflect"
	"unicode/utf8"
)

// Validate the JSON-schema subset used by processing stages before reusing a
// provider response. Semantic/source validation still belongs to the caller.
func reusable(content, schema json.RawMessage) bool {
	var value any
	if json.Unmarshal(content, &value) != nil || hasReadingIssues(value) {
		return false
	}
	if len(schema) == 0 {
		return true
	}
	var rules map[string]any
	if json.Unmarshal(schema, &rules) != nil {
		return false
	}
	return matchesShape(value, rules)
}
func hasReadingIssues(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if issues, ok := item.([]any); k == "issues" && ok && len(issues) > 0 {
				return true
			}
			if hasReadingIssues(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if hasReadingIssues(item) {
				return true
			}
		}
	}
	return false
}
func matchesShape(value any, rules map[string]any) bool {
	if enum, ok := rules["enum"].([]any); ok {
		found := false
		for _, v := range enum {
			found = found || reflect.DeepEqual(v, value)
		}
		if !found {
			return false
		}
	}
	switch rules["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return false
		}
		props, _ := rules["properties"].(map[string]any)
		if required, ok := rules["required"].([]any); ok {
			for _, key := range required {
				s, ok := key.(string)
				if !ok {
					return false
				}
				if _, exists := obj[s]; !exists {
					return false
				}
			}
		}
		for key, item := range obj {
			if nested, ok := props[key].(map[string]any); ok {
				if !matchesShape(item, nested) {
					return false
				}
			} else if rules["additionalProperties"] == false {
				return false
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return false
		}
		if n, ok := rules["minItems"].(float64); ok && len(items) < int(n) {
			return false
		}
		if n, ok := rules["maxItems"].(float64); ok && len(items) > int(n) {
			return false
		}
		if nested, ok := rules["items"].(map[string]any); ok {
			for _, item := range items {
				if !matchesShape(item, nested) {
					return false
				}
			}
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return false
		}
		if n, ok := rules["minLength"].(float64); ok && utf8.RuneCountInString(s) < int(n) {
			return false
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return false
		}
	case "number", "integer":
		n, ok := value.(float64)
		if !ok {
			return false
		}
		if rules["type"] == "integer" && math.Trunc(n) != n {
			return false
		}
		if minimum, ok := rules["minimum"].(float64); ok && n < minimum {
			return false
		}
	}
	return true
}
