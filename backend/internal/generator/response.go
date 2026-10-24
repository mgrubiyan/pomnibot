package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// AIResponse decodes the private model contract into expanded facts for validation.
type AIResponse struct {
	Facts  []Fact   `json:"facts"`
	Issues []string `json:"issues"`
}

// Fact groups cards in ascending difficulty, preserving model order within a kind.
type Fact struct {
	Name  string `json:"name"`
	Pages []int  `json:"pages"`
	Cards []Card `json:"cards"`

	// Keep legacy wire indices and model paths after the stable difficulty sort.
	cardIndices []int
	cardPaths   []string
}

// Card is one of the five supported question formats.
type Card interface {
	isCard()
	Base() CardBase
}

// CardBase holds the question and source specific to one card.
type CardBase struct {
	Question    string `json:"question"`
	Source      string `json:"source"`
	SourceQuote string `json:"sourceQuote"`
}

// FlipCard presents an answer for self assessment.
type FlipCard struct {
	CardBase
	Answer string `json:"answer"`
}

// BooleanCard asks whether a statement is true.
type BooleanCard struct {
	CardBase
	Answer bool `json:"answer"`
}

// ChoiceCard supports multiple correct answers.
type ChoiceCard struct {
	CardBase
	RightAnswers []string `json:"rightAnswers"`
	WrongAnswers []string `json:"wrongAnswers"`
}

// TableCard groups rows into named categories.
type TableCard struct {
	CardBase
	Columns []Column `json:"columns"`
}

// InputCard asks for a typed answer.
type InputCard struct {
	CardBase
	Answer string `json:"answer"`
}

// Column contains one category and its rows.
type Column struct {
	Name string   `json:"name"`
	Rows []string `json:"rows"`
}

func (FlipCard) isCard()    {}
func (BooleanCard) isCard() {}
func (ChoiceCard) isCard()  {}
func (TableCard) isCard()   {}
func (InputCard) isCard()   {}

// Base returns the metadata shared by all card formats.
func (b CardBase) Base() CardBase { return b }

// CardCount counts cards across nested facts.
func CardCount(facts []Fact) int {
	total := 0
	for _, f := range facts {
		total += len(f.Cards)
	}
	return total
}

// UnmarshalJSON decodes the discriminated union and stably orders card kinds.
func (f *Fact) UnmarshalJSON(b []byte) error {
	var wire struct {
		Name  string            `json:"name"`
		Pages []int             `json:"pages"`
		Cards []json.RawMessage `json:"cards"`
	}
	if err := requiredFields(b, "name", "pages", "cards"); err != nil {
		return err
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	*f = Fact{Name: wire.Name, Pages: wire.Pages}
	flip, input := false, false
	for i, raw := range wire.Cards {
		path := fmt.Sprintf("cards[%d]", i)
		c, err := decodeCard(raw)
		if err != nil {
			return atPath(path, err)
		}
		switch c.(type) {
		case FlipCard:
			flip = true
		case InputCard:
			input = true
		}
		f.Cards = append(f.Cards, c)
	}
	if !flip {
		return problem("validation", "cards", "missing required flip card")
	}
	if !input {
		return problem("validation", "cards", "missing required input card")
	}
	f.cardIndices = make([]int, len(f.Cards))
	for i := range f.cardIndices {
		f.cardIndices[i] = i
	}
	slices.SortStableFunc(f.cardIndices, func(a, b int) int { return cardDifficulty(f.Cards[a]) - cardDifficulty(f.Cards[b]) })
	cards := slices.Clone(f.Cards)
	for i, original := range f.cardIndices {
		f.Cards[i] = cards[original]
	}
	return nil
}

func decodeCard(raw json.RawMessage) (Card, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, shapeProblem("", "object", err)
	}
	if err := requiredFields(raw, "kind"); err != nil {
		return nil, atPath("", err)
	}
	var kind string
	if err := json.Unmarshal(fields["kind"], &kind); err != nil {
		return nil, shapeProblem("kind", "string", err)
	}
	delete(fields, "kind")
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			// An unknown field name can itself contain private response text.
			if !knownCardField(key) {
				key = "<unknown>"
			}
			return nil, problem("null_field", key, "card field is null")
		}
	}
	content, err := json.Marshal(fields)
	if err != nil {
		return nil, atPath("", err)
	}
	var c Card
	switch kind {
	case "flip":
		var v FlipCard
		err = strictJSON(content, &v)
		c = v
	case "boolean":
		var v BooleanCard
		err = strictJSON(content, &v)
		c = v
	case "choice":
		var v ChoiceCard
		err = strictJSON(content, &v)
		c = v
	case "table":
		var v TableCard
		err = strictJSON(content, &v)
		c = v
	case "input":
		var v InputCard
		err = strictJSON(content, &v)
		c = v
	default:
		return nil, problem("invalid_structure", "kind", "unknown card kind")
	}
	if err != nil {
		return nil, atPath("", err)
	}
	for _, key := range []string{"question", "source", "sourceQuote"} {
		if _, ok := fields[key]; !ok {
			return nil, problem("missing_field", key, "required field is missing")
		}
	}
	if kind == "flip" || kind == "boolean" || kind == "input" {
		if _, ok := fields["answer"]; !ok {
			return nil, problem("missing_field", "answer", "required field is missing")
		}
	}
	return c, nil
}

func knownCardField(key string) bool {
	switch key {
	case "question", "source", "sourceQuote", "answer", "rightAnswers", "wrongAnswers", "columns":
		return true
	default:
		return false
	}
}

func cardDifficulty(c Card) int {
	switch c.(type) {
	case FlipCard:
		return 0
	case BooleanCard:
		return 1
	case ChoiceCard:
		return 2
	case TableCard:
		return 3
	default:
		return 4
	}
}

// MarshalJSON includes the wire discriminator without adding it to the DTOs.
func (f Fact) MarshalJSON() ([]byte, error) {
	raw := make([]map[string]any, 0, len(f.Cards))
	kinds := []string{"flip", "boolean", "choice", "table", "input"}
	for _, c := range f.Cards {
		b, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		var v map[string]any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		v["kind"] = kinds[cardDifficulty(c)]
		raw = append(raw, v)
	}
	return json.Marshal(struct {
		Name  string           `json:"name"`
		Pages []int            `json:"pages"`
		Cards []map[string]any `json:"cards"`
	}{f.Name, f.Pages, raw})
}

// UnmarshalJSON rejects missing, null and legacy response fields.
func (r *AIResponse) UnmarshalJSON(b []byte) error {
	if err := requiredFields(b, "facts", "issues"); err != nil {
		return err
	}
	var wire struct {
		Facts  responseFacts `json:"facts"`
		Issues []string      `json:"issues"`
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	*r = AIResponse{Issues: wire.Issues, Facts: wire.Facts}
	return nil
}

func requiredFields(b []byte, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return shapeProblem("", "object", err)
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok {
			return problem("missing_field", key, "required field is missing")
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return problem("null_field", key, "required field is null")
		}
	}
	return nil
}

func normalized(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func validateFacts(facts []Fact, pages int) error {
	if len(facts) == 0 {
		return problem("validation", "facts", "facts are empty")
	}
	names, questions := map[string]bool{}, map[string]bool{}
	for i, f := range facts {
		path := fmt.Sprintf("facts[%d]", i)
		name := normalized(f.Name)
		if name == "" {
			return problem("validation", path+".name", "fact name is empty")
		}
		if names[name] {
			return problem("validation", path+".name", "duplicate fact name")
		}
		names[name] = true
		if len(f.Pages) == 0 {
			return problem("validation", path+".pages", "fact pages are empty")
		}
		seen := map[int]bool{}
		for j, p := range f.Pages {
			pagePath := fmt.Sprintf("%s.pages[%d]", path, j)
			if p < 1 || p > pages {
				return problem("validation", pagePath, fmt.Sprintf("page is outside document range 1..%d", pages))
			}
			if seen[p] {
				return problem("validation", pagePath, "duplicate fact page")
			}
			seen[p] = true
		}
		flip, input := false, false
		for j, c := range f.Cards {
			index := j
			if len(f.cardIndices) == len(f.Cards) {
				index = f.cardIndices[j]
			}
			cardPath := fmt.Sprintf("%s.cards[%d]", path, index)
			if len(f.cardPaths) == len(f.Cards) {
				cardPath = fieldPath(path, f.cardPaths[j])
			}
			if err := validateCard(c); err != nil {
				return atPath(cardPath, err)
			}
			key := fmt.Sprintf("%T:%s", c, normalized(c.Base().Question))
			if questions[key] {
				return problem("validation", cardPath+".question", "duplicate card question of the same kind")
			}
			questions[key] = true
			switch c.(type) {
			case FlipCard:
				flip = true
			case InputCard:
				input = true
			}
		}
		if !flip {
			return problem("validation", path+".cards", "missing required flip card")
		}
		if !input {
			return problem("validation", path+".cards", "missing required input card")
		}
	}
	return nil
}

// validateCard checks content without creating storage models.
func validateCard(c Card) error {
	if c == nil {
		return problem("validation", "", "card is null")
	}
	base := c.Base()
	if normalized(base.Question) == "" {
		return problem("validation", "question", "question is empty")
	}
	if normalized(base.Source) == "" {
		return problem("validation", "source", "source is empty")
	}
	switch v := c.(type) {
	case FlipCard:
		if normalized(v.Answer) == "" {
			return problem("validation", "answer", "answer is empty")
		}
	case BooleanCard:
	case InputCard:
		if normalized(v.Answer) == "" {
			return problem("validation", "answer", "answer is empty")
		}
	case ChoiceCard:
		if len(v.RightAnswers) == 0 {
			return problem("validation", "rightAnswers", "right answers are empty")
		}
		if len(v.WrongAnswers) == 0 {
			return problem("validation", "wrongAnswers", "wrong answers are empty")
		}
		seen := map[string]bool{}
		for _, answers := range []struct {
			field  string
			values []string
		}{{"rightAnswers", v.RightAnswers}, {"wrongAnswers", v.WrongAnswers}} {
			for i, a := range answers.values {
				path := fmt.Sprintf("%s[%d]", answers.field, i)
				key := normalized(a)
				if key == "" {
					return problem("validation", path, "choice answer is empty")
				}
				if seen[key] {
					return problem("validation", path, "duplicate choice answer")
				}
				seen[key] = true
			}
		}
	case TableCard:
		if len(v.Columns) < 2 {
			return problem("validation", "columns", "table requires at least two categories")
		}
		names, rows := map[string]bool{}, map[string]bool{}
		for i, col := range v.Columns {
			path := fmt.Sprintf("columns[%d]", i)
			name := strings.TrimSpace(col.Name)
			if name == "" {
				return problem("validation", path+".name", "table category name is empty")
			}
			if names[name] {
				return problem("validation", path+".name", "duplicate table category")
			}
			if len(col.Rows) == 0 {
				return problem("validation", path+".rows", "table rows are empty")
			}
			names[name] = true
			for j, row := range col.Rows {
				row = strings.TrimSpace(row)
				rowPath := fmt.Sprintf("%s.rows[%d]", path, j)
				if row == "" {
					return problem("validation", rowPath, "table row is empty")
				}
				if rows[row] {
					return problem("validation", rowPath, "duplicate table row")
				}
				rows[row] = true
			}
		}
	default:
		return problem("validation", "kind", "unknown card kind")
	}
	return nil
}
