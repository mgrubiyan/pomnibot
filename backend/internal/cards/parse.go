package cards

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// modelCard is one card as the model returned it, before quote and
// distractor checks.
type modelCard struct {
	Kind        Kind
	Question    string
	Answer      string
	Explanation string
	Quote       string
	Topic       string
}

var (
	errNoJSON  = errors.New("no JSON in the answer")
	errNoCards = errors.New(`answer has no "cards" array`)

	reFence = regexp.MustCompile("(?s)```[a-zA-Z]*\\s*(.*?)```")
	bom     = []byte("\uFEFF")
)

// parseAnswer extracts the JSON from a model answer and checks it against
// cardSchema. Structured output may be unsupported or ignored by the model,
// so nothing is assumed: the answer may be wrapped in a markdown block or in
// prose.
//
// An error means the answer as a whole is unusable and is worth a retry.
// Single cards that break the schema are dropped and counted in invalid, so
// one bad card does not cost the other two.
func parseAnswer(raw []byte, maxCards int) (cards []modelCard, invalid int, err error) {
	data, err := extractJSON(raw)
	if err != nil {
		return nil, 0, err
	}

	var items []json.RawMessage
	switch data[0] {
	case '[':
		// A bare array instead of {"cards": [...]}: the intent is clear.
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, 0, fmt.Errorf("decode cards: %w", err)
		}
	case '{':
		var obj struct {
			Cards *[]json.RawMessage `json:"cards"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, 0, fmt.Errorf("decode answer: %w", err)
		}
		if obj.Cards == nil {
			return nil, 0, errNoCards
		}
		items = *obj.Cards
	default:
		return nil, 0, errNoJSON
	}

	for i, item := range items {
		if i >= maxCards {
			invalid += len(items) - maxCards
			break
		}
		c, ok := parseCard(item)
		if !ok {
			invalid++
			continue
		}
		cards = append(cards, c)
	}
	return cards, invalid, nil
}

func parseCard(data json.RawMessage) (modelCard, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return modelCard{}, false
	}
	get := func(name string) (string, bool) {
		var s string
		raw, ok := fields[name]
		if !ok || json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		s = strings.TrimSpace(s)
		return s, s != ""
	}

	var c modelCard
	var kind string
	for _, f := range []struct {
		name string
		dst  *string
	}{
		{fieldKind, &kind},
		{fieldQuestion, &c.Question},
		{fieldAnswer, &c.Answer},
		{fieldExplanation, &c.Explanation},
		{fieldQuote, &c.Quote},
		{fieldTopic, &c.Topic},
	} {
		v, ok := get(f.name)
		if !ok {
			return modelCard{}, false
		}
		*f.dst = v
	}

	c.Kind = Kind(strings.ToLower(kind))
	switch c.Kind {
	case KindChoice, KindFlip, KindInput:
	case KindBoolean:
		answer, ok := parseBool(c.Answer)
		if !ok {
			return modelCard{}, false
		}
		c.Answer = answer
	default:
		return modelCard{}, false
	}
	return c, true
}

func parseBool(s string) (string, bool) {
	switch strings.ToLower(strings.Trim(s, " .!")) {
	case "true", "верно", "да", "правда":
		return AnswerTrue, true
	case "false", "неверно", "нет", "ложь":
		return AnswerFalse, true
	}
	return "", false
}

// extractJSON finds the JSON value in a model answer: the whole answer, a
// fenced markdown block, or the first balanced object or array in prose.
func extractJSON(raw []byte) ([]byte, error) {
	s := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(raw), bom))
	if len(s) == 0 {
		return nil, errNoJSON
	}
	if json.Valid(s) {
		return s, nil
	}
	for _, m := range reFence.FindAllSubmatch(s, -1) {
		if block := bytes.TrimSpace(m[1]); len(block) > 0 && json.Valid(block) {
			return block, nil
		}
	}
	// Objects first: prose may contain "[1]" before the real answer.
	for _, open := range []byte{'{', '['} {
		if v := firstBalanced(s, open); v != nil {
			return v, nil
		}
	}
	return nil, errNoJSON
}

// firstBalanced returns the first valid JSON value in s that starts with
// open, tracking nesting and skipping brackets inside strings.
func firstBalanced(s []byte, open byte) []byte {
	for from := 0; from < len(s); {
		i := bytes.IndexByte(s[from:], open)
		if i < 0 {
			return nil
		}
		start := from + i
		if end := balancedEnd(s, start); end > 0 && json.Valid(s[start:end]) {
			return s[start:end]
		}
		from = start + 1
	}
	return nil
}

func balancedEnd(s []byte, start int) int {
	depth, inString, escaped := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inString:
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
