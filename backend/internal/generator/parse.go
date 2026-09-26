package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// modelFact is one fact as the model returned it: a quote and the cards that
// test it in different forms. After the quote check, Quote holds the span of
// the notes and ID identifies the fact.
type modelFact struct {
	ID    string
	Quote string
	Topic string
	Name  string
	Cards []modelCard
}

// modelCard is one card of a fact as the model returned it.
type modelCard struct {
	Kind        cards.Kind
	Question    string
	Answer      string
	Explanation string
}

// maxCardsPerFact is one card of each kind.
const maxCardsPerFact = 4

var (
	errNoJSON  = errors.New("no JSON in the answer")
	errNoFacts = errors.New(`answer has no "facts" array`)

	reFence = regexp.MustCompile("(?s)```[a-zA-Z]*\\s*(.*?)```")
	bom     = []byte("\uFEFF")
)

// parseAnswer extracts the JSON from a model answer and checks it against
// cardSchema. Structured output may be unsupported or ignored by the model,
// so nothing is assumed: the answer may be wrapped in a markdown block or in
// prose, and it may come in the flat {"cards": [...]} form, where every card
// carries its own quote and becomes a fact of its own.
//
// An error means the answer as a whole is unusable and is worth a retry.
// Cards that break the schema are dropped one by one and counted in invalid,
// so one bad card does not cost the rest.
func parseAnswer(raw []byte, maxFacts int) (facts []modelFact, invalid int, err error) {
	candidates := jsonCandidates(raw)
	if len(candidates) == 0 {
		return nil, 0, errNoJSON
	}
	// The first candidate with facts wins. Without any, a clean empty answer
	// ({"facts": []}) beats a stray value such as "[1]" from the prose.
	found := false
	for _, data := range candidates {
		f, n, derr := decodeAnswer(data, maxFacts)
		switch {
		case derr != nil:
			continue
		case len(f) > 0:
			return f, n, nil
		case !found || invalid > 0 && n == 0:
			facts, invalid, found = f, n, true
		}
	}
	if !found {
		return nil, 0, errNoFacts
	}
	return facts, invalid, nil
}

// decodeAnswer reads one JSON value as a set of facts.
func decodeAnswer(data []byte, maxFacts int) ([]modelFact, int, error) {
	var items []json.RawMessage
	flat := false
	switch data[0] {
	case '[':
		// A bare array: of facts, or of flat cards.
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, 0, fmt.Errorf("decode answer: %w", err)
		}
		flat = len(items) > 0 && !hasField(items[0], "cards")
	case '{':
		var obj struct {
			Facts *[]json.RawMessage `json:"facts"`
			Cards *[]json.RawMessage `json:"cards"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, 0, fmt.Errorf("decode answer: %w", err)
		}
		switch {
		case obj.Facts != nil:
			items = *obj.Facts
		case obj.Cards != nil:
			items, flat = *obj.Cards, true
		default:
			return nil, 0, errNoFacts
		}
	default:
		return nil, 0, errNoFacts
	}

	if flat {
		facts, invalid := groupFlatCards(items, maxFacts)
		return facts, invalid, nil
	}
	var facts []modelFact
	invalid := 0
	for i, item := range items {
		if i >= maxFacts {
			for _, rest := range items[i:] {
				invalid += countCards(rest)
			}
			break
		}
		f, bad := parseFact(item)
		invalid += bad
		if len(f.Cards) > 0 {
			facts = append(facts, f)
		}
	}
	return facts, invalid, nil
}

// groupFlatCards reads the flat form, where every card carries its own quote
// and topic. Cards with the same quote are variants of one fact.
func groupFlatCards(items []json.RawMessage, maxFacts int) ([]modelFact, int) {
	var facts []modelFact
	byQuote := map[string]int{}
	invalid := 0
	for _, item := range items {
		f, bad := parseFlatCard(item)
		if bad > 0 {
			invalid += bad
			continue
		}
		i, seen := byQuote[f.Quote]
		switch {
		case seen && len(facts[i].Cards) < maxCardsPerFact:
			facts[i].Cards = append(facts[i].Cards, f.Cards...)
		case seen || len(facts) >= maxFacts:
			invalid++
		default:
			byQuote[f.Quote] = len(facts)
			facts = append(facts, f)
		}
	}
	return facts, invalid
}

// parseFact reads {"quote", "topic", "name", "cards": [...]}. A fact without
// a quote or topic loses all its cards: nothing can be checked without the
// quote. A missing name is not worth the cards: the topic stands in for it.
func parseFact(data json.RawMessage) (modelFact, int) {
	fields, ok := objectFields(data)
	if !ok {
		return modelFact{}, 1
	}
	var items []json.RawMessage
	if raw, ok := fields["cards"]; !ok || json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return modelFact{}, 1
	}
	quote, okQuote := stringField(fields, fieldQuote)
	topic, okTopic := stringField(fields, fieldTopic)
	if !okQuote || !okTopic {
		return modelFact{}, len(items)
	}

	f := modelFact{Quote: quote, Topic: topic, Name: factName(fields, topic)}
	invalid := 0
	for i, item := range items {
		if i >= maxCardsPerFact {
			invalid += len(items) - maxCardsPerFact
			break
		}
		cardFields, ok := objectFields(item)
		if !ok {
			invalid++
			continue
		}
		c, ok := parseCard(cardFields)
		if !ok {
			invalid++
			continue
		}
		f.Cards = append(f.Cards, c)
	}
	return f, invalid
}

// parseFlatCard reads a card of the flat form, with its own quote and topic,
// as a fact with one card.
func parseFlatCard(data json.RawMessage) (modelFact, int) {
	fields, ok := objectFields(data)
	if !ok {
		return modelFact{}, 1
	}
	quote, okQuote := stringField(fields, fieldQuote)
	topic, okTopic := stringField(fields, fieldTopic)
	c, okCard := parseCard(fields)
	if !okQuote || !okTopic || !okCard {
		return modelFact{}, 1
	}
	return modelFact{Quote: quote, Topic: topic, Name: factName(fields, topic), Cards: []modelCard{c}}, 0
}

// factName is the fact's name, or its topic when the model gave none.
func factName(fields map[string]json.RawMessage, topic string) string {
	if name, ok := stringField(fields, fieldName); ok {
		return name
	}
	return topic
}

func parseCard(fields map[string]json.RawMessage) (modelCard, bool) {
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
	} {
		v, ok := stringField(fields, f.name)
		if !ok {
			return modelCard{}, false
		}
		*f.dst = v
	}

	c.Kind = cards.Kind(strings.ToLower(kind))
	switch c.Kind {
	case cards.KindChoice, cards.KindFlip, cards.KindInput:
	case cards.KindBoolean:
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

// stringField reads a non-empty field. Numbers and booleans are taken as
// their text: without strict structured output models write "answer": true
// or "answer": 1961 as often as strings.
func stringField(fields map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := fields[name]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var scalar any
		if json.Unmarshal(raw, &scalar) != nil {
			return "", false
		}
		switch scalar.(type) {
		case bool, float64:
			s = string(raw)
		default:
			return "", false
		}
	}
	s = strings.TrimSpace(s)
	return s, s != ""
}

func objectFields(data json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func hasField(data json.RawMessage, name string) bool {
	fields, ok := objectFields(data)
	_, has := fields[name]
	return ok && has
}

// countCards is how many cards an unused fact held, for the stats.
func countCards(item json.RawMessage) int {
	fields, ok := objectFields(item)
	if !ok {
		return 1
	}
	var cards []json.RawMessage
	if raw, ok := fields["cards"]; ok && json.Unmarshal(raw, &cards) == nil && len(cards) > 0 {
		return min(len(cards), maxCardsPerFact)
	}
	return 1
}

func parseBool(s string) (string, bool) {
	switch strings.ToLower(strings.Trim(s, " .!")) {
	case "true", "верно", "да", "правда":
		return cards.AnswerTrue, true
	case "false", "неверно", "нет", "ложь":
		return cards.AnswerFalse, true
	}
	return "", false
}

// jsonCandidates lists the JSON values a model answer may carry, most likely
// first: the whole answer, fenced markdown blocks, then every balanced object
// or array in the text in order of appearance. The caller takes the first one
// that decodes as an answer, so "[1]" in prose or a bare array of cards after
// "Вот карточки:" do not get in the way.
func jsonCandidates(raw []byte) [][]byte {
	s := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(raw), bom))
	if len(s) == 0 {
		return nil
	}
	var out [][]byte
	if json.Valid(s) {
		out = append(out, s)
	}
	for _, m := range reFence.FindAllSubmatch(s, -1) {
		if block := bytes.TrimSpace(m[1]); len(block) > 0 && json.Valid(block) {
			out = append(out, block)
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '{' && s[i] != '[' {
			continue
		}
		if end := balancedEnd(s, i); end > 0 && json.Valid(s[i:end]) {
			out = append(out, s[i:end])
			i = end - 1
		}
	}
	return out
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
