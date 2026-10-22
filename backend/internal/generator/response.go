package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// AIResponse is the model's learning content. Source metadata lives separately.
type AIResponse struct {
	Facts []Fact `json:"facts"`
}

// Fact groups cards in ascending difficulty, preserving model order within a kind.
type Fact struct {
	Name  string `json:"name"`
	Cards []Card `json:"cards"`
}

// Card is one of the five supported question formats.
type Card interface{ isCard() }

// CardBase holds the question shared by all formats.
type CardBase struct {
	Question string `json:"question"`
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

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// UnmarshalJSON decodes the discriminated union and stably orders card kinds.
func (f *Fact) UnmarshalJSON(b []byte) error {
	var wire struct {
		Name  string            `json:"name"`
		Cards []json.RawMessage `json:"cards"`
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	*f = Fact{Name: wire.Name}
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
		if _, ok := fields["question"]; !ok {
			return fmt.Errorf("missing question")
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
		Cards []map[string]any `json:"cards"`
	}{f.Name, raw})
}
