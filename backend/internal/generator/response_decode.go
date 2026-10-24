package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return safeDecodeProblem(err)
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

// encoding/json errors may include arbitrary response keys or values. Keep
// only trusted schema field names and the expected Go type, never the raw text.
func safeDecodeProblem(err error) error {
	var detail *diagnosticError
	if errors.As(err, &detail) {
		return err
	}
	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &mismatch) {
		path := ""
		for _, field := range strings.Split(mismatch.Field, ".") {
			if index, e := strconv.Atoi(field); e == nil && index >= 0 {
				path += fmt.Sprintf("[%d]", index)
				continue
			}
			name := "<unknown>"
			for _, known := range []string{"facts", "issues", "name", "pages", "cards", "kind", "question", "source", "sourceQuote", "answer", "rightAnswers", "wrongAnswers", "columns", "rows"} {
				if strings.EqualFold(field, known) {
					name = known
					break
				}
			}
			path = fieldPath(path, name)
		}
		expected := mismatch.Type.Kind().String()
		switch expected {
		case "struct":
			expected = "object"
		case "slice":
			expected = "array"
		case "int":
			expected = "integer"
		case "bool":
			expected = "boolean"
		}
		if mismatch.Field == "" {
			path = ""
		}
		return shapeProblem(path, expected, err)
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		detail = problem("invalid_structure", "<unknown>", "unknown field")
		detail.cause = err
		return detail
	}
	return atPath("", err)
}

func shapeProblem(path, expected string, cause error) error {
	return &diagnosticError{category: "invalid_structure", path: path, reason: "expected " + expected, cause: cause}
}
