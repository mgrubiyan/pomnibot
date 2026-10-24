package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// AIResponse is the complete model response, before storage identifiers exist.
type AIResponse struct {
	Facts  []Fact   `json:"facts"`
	Issues []string `json:"issues"`
}

// Fact groups cards in ascending difficulty, preserving model order within a kind.
type Fact struct {
	Name  string `json:"name"`
	Pages []int  `json:"pages"`
	Cards []Card `json:"cards"`
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

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
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
	for _, raw := range wire.Cards {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		var kind string
		if err := json.Unmarshal(fields["kind"], &kind); err != nil {
			return err
		}
		delete(fields, "kind")
		for key, value := range fields {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("null card field %s", key)
			}
		}
		content, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		var c Card
		switch kind {
		case "flip":
			var v FlipCard
			err = strictJSON(content, &v)
			c = v
			flip = true
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
			input = true
		default:
			return fmt.Errorf("unknown card kind %q", kind)
		}
		if err != nil {
			return err
		}
		for _, key := range []string{"question", "source", "sourceQuote"} {
			if _, ok := fields[key]; !ok {
				return fmt.Errorf("missing card field %s", key)
			}
		}
		if kind == "flip" || kind == "boolean" || kind == "input" {
			if _, ok := fields["answer"]; !ok {
				return fmt.Errorf("missing answer")
			}
		}
		f.Cards = append(f.Cards, c)
	}
	if !flip {
		return fmt.Errorf("fact %q needs a flip card", f.Name)
	}
	if !input {
		return fmt.Errorf("fact %q needs an input card", f.Name)
	}
	slices.SortStableFunc(f.Cards, func(a, b Card) int { return cardDifficulty(a) - cardDifficulty(b) })
	return nil
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
	type wire AIResponse
	return strictJSON(b, (*wire)(r))
}

func requiredFields(b []byte, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("missing field %s", key)
		}
	}
	return nil
}

func normalized(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func validateFacts(facts []Fact, pages int) error {
	if len(facts) == 0 {
		return errors.New("empty facts")
	}
	names, questions := map[string]bool{}, map[string]bool{}
	for _, f := range facts {
		name := normalized(f.Name)
		if name == "" || names[name] {
			return errors.New("invalid or duplicate fact name")
		}
		names[name] = true
		if len(f.Pages) == 0 {
			return errors.New("empty fact pages")
		}
		seen := map[int]bool{}
		for _, p := range f.Pages {
			if p < 1 || p > pages || seen[p] {
				return errors.New("invalid fact pages")
			}
			seen[p] = true
		}
		flip, input := false, false
		for _, c := range f.Cards {
			if err := validateCard(c); err != nil {
				return err
			}
			key := fmt.Sprintf("%T:%s", c, normalized(c.Base().Question))
			if questions[key] {
				return errors.New("duplicate card question of the same kind")
			}
			questions[key] = true
			switch c.(type) {
			case FlipCard:
				flip = true
			case InputCard:
				input = true
			}
		}
		if !flip || !input {
			return errors.New("fact requires flip and input")
		}
	}
	return nil
}

// validateCard checks content without creating storage models.
func validateCard(c Card) error {
	if c == nil {
		return errors.New("invalid card")
	}
	base := c.Base()
	if normalized(base.Question) == "" || normalized(base.Source) == "" {
		return errors.New("empty card question or source")
	}
	switch v := c.(type) {
	case FlipCard:
		if normalized(v.Answer) == "" {
			return errors.New("empty flip answer")
		}
	case BooleanCard:
	case InputCard:
		if normalized(v.Answer) == "" {
			return errors.New("empty input answer")
		}
	case ChoiceCard:
		if len(v.RightAnswers) == 0 || len(v.WrongAnswers) == 0 {
			return errors.New("choice requires right and wrong answers")
		}
		seen := map[string]bool{}
		for _, a := range append(slices.Clone(v.RightAnswers), v.WrongAnswers...) {
			key := normalized(a)
			if key == "" || seen[key] {
				return errors.New("empty or duplicate choice answer")
			}
			seen[key] = true
		}
	case TableCard:
		if len(v.Columns) < 2 {
			return errors.New("table requires two categories")
		}
		names, rows := map[string]bool{}, map[string]bool{}
		for _, col := range v.Columns {
			name := strings.TrimSpace(col.Name)
			if name == "" || names[name] || len(col.Rows) == 0 {
				return errors.New("empty or duplicate table category")
			}
			names[name] = true
			for _, row := range col.Rows {
				row = strings.TrimSpace(row)
				if row == "" || rows[row] {
					return errors.New("empty or duplicate table row")
				}
				rows[row] = true
			}
		}
	default:
		return errors.New("unknown card kind")
	}
	return nil
}
