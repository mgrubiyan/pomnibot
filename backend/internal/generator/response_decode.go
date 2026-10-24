package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func decodeResponse(resp providers.Response, reply *AIResponse) error {
	if resp.Truncated {
		return problem("truncated", "", "response was truncated")
	}
	if len(bytes.TrimSpace(resp.Content)) == 0 {
		return problem("empty_response", "", "response is empty")
	}
	// Validate the entire response first, including trailing content. SyntaxError
	// messages may quote private characters, so retain only the numeric position.
	var raw json.RawMessage
	if err := json.Unmarshal(resp.Content, &raw); err != nil {
		detail := problem("invalid_json", "", "invalid JSON")
		detail.cause = err
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			detail.jsonOffset = syntax.Offset
		}
		return detail
	}
	if err := json.Unmarshal(resp.Content, reply); err != nil {
		return err
	}
	if reply.Issues == nil {
		return problem("null_field", "issues", "required field is null")
	}
	if reply.Facts == nil {
		return problem("null_field", "facts", "required field is null")
	}
	return nil
}

func strictJSON(b []byte, v any) error {
	if err := checkShape(b, reflect.TypeOf(v).Elem(), ""); err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return safeDecodeProblem(err, reflect.TypeOf(v).Elem())
	}
	return nil
}

// A custom slice decoder keeps fact indices without deferring decoding until
// after the whole object: earlier duplicate facts fields must still be checked,
// as they were by encoding/json before diagnostics were added.
type responseFacts []Fact

func (facts *responseFacts) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return shapeProblem("facts", "array", err)
	}
	if raw == nil {
		*facts = nil
		return nil
	}
	decoded := make(responseFacts, 0, len(raw))
	for i, item := range raw {
		var fact Fact
		if err := json.Unmarshal(item, &fact); err != nil {
			return atPath(fmt.Sprintf("facts[%d]", i), err)
		}
		decoded = append(decoded, fact)
	}
	*facts = decoded
	return nil
}

func safeDecodeProblem(err error, typ reflect.Type) error {
	var detail *diagnosticError
	if errors.As(err, &detail) {
		return err
	}
	var mismatch *json.UnmarshalTypeError
	if !errors.As(err, &mismatch) {
		return atPath("", err)
	}
	// The initial shape check sees the final value of duplicate keys. If an
	// earlier value fails in encoding/json, recover only its trusted DTO path.
	path := ""
	for _, part := range strings.Split(mismatch.Field, ".") {
		for typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			break
		}
		fields := jsonFields(typ)
		index := slices.IndexFunc(fields, func(f reflect.StructField) bool { return strings.EqualFold(part, f.Tag.Get("json")) })
		if index < 0 {
			break
		}
		field := fields[index]
		path = fieldPath(path, field.Tag.Get("json"))
		typ = field.Type
	}
	return shapeProblem(path, expectedJSONType(mismatch.Type), err)
}

func expectedJSONType(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Struct:
		return "object"
	case reflect.Slice:
		return "array"
	case reflect.Int:
		return "integer"
	case reflect.Bool:
		return "boolean"
	default:
		return typ.Kind().String()
	}
}

// checkShape uses only DTO field names in paths, never untrusted object keys or
// values from encoding/json errors. RawMessages are decoded with context by the
// response/fact unmarshallers. Null handling remains with existing required-field
// and card checks; optional nested null values keep encoding/json semantics.
func checkShape(b []byte, typ reflect.Type, path string) error {
	if typ == reflect.TypeFor[json.RawMessage]() || typ == reflect.TypeFor[responseFacts]() || bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			return shapeProblem(path, "object", err)
		}
		known := jsonFields(typ)
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			index := slices.IndexFunc(known, func(f reflect.StructField) bool { return strings.EqualFold(key, f.Tag.Get("json")) })
			if index < 0 {
				return problem("invalid_structure", fieldPath(path, "<unknown>"), "unknown field")
			}
			field := known[index]
			if err := checkShape(fields[key], field.Type, fieldPath(path, field.Tag.Get("json"))); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if err := json.Unmarshal(b, &items); err != nil {
			return shapeProblem(path, "array", err)
		}
		for i, item := range items {
			if err := checkShape(item, typ.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		if err := json.Unmarshal(b, reflect.New(typ).Interface()); err != nil {
			return shapeProblem(path, expectedJSONType(typ), err)
		}
	}
	return nil
}

func jsonFields(typ reflect.Type) []reflect.StructField {
	var fields []reflect.StructField
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			fields = append(fields, jsonFields(field.Type)...)
		} else if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
			fields = append(fields, field)
		}
	}
	return fields
}

func shapeProblem(path, expected string, cause error) error {
	return &diagnosticError{category: "invalid_structure", path: path, reason: "expected " + expected, cause: cause}
}
