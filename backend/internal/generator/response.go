package generator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"slices"
	"strconv"
	"strings"
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

// KnowledgeGoal preserves sufficient source information, not a page transcript.
type KnowledgeGoal struct {
	GoalID string `json:"goalId"`
	Name   string `json:"name"`
	Topic  string `json:"topic"`
	Pages  []int  `json:"pages"`
	Source string `json:"source"`
}
type goalCards struct {
	GoalID  string `json:"goalId"`
	Content Fact   `json:"content"`
}
type stageResponse struct {
	Issues    []string        `json:"issues"`
	Goals     []KnowledgeGoal `json:"goals"`
	Cards     []goalCards     `json:"cards"`
	Complete  *bool           `json:"complete"`
	Additions []KnowledgeGoal `json:"additions"`
}

func normalized(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func validateGoals(goals []KnowledgeGoal, pages int) error {
	if len(goals) == 0 {
		return errors.New("empty goals")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, g := range goals {
		if g.GoalID == "" || ids[g.GoalID] || names[normalized(g.Name)] || normalized(g.Name) == "" || normalized(g.Topic) == "" || normalized(g.Source) == "" || len(g.Pages) == 0 {
			return errors.New("invalid or duplicate goal")
		}
		ids[g.GoalID], names[normalized(g.Name)] = true, true
		seen := map[int]bool{}
		for _, p := range g.Pages {
			if p < 1 || p > pages || seen[p] {
				return errors.New("invalid goal pages")
			}
			seen[p] = true
		}
	}
	return nil
}
func validateGoalCards(goals []KnowledgeGoal, groups []goalCards) error {
	if len(groups) != len(goals) {
		return errors.New("incomplete goal coverage")
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	questions := map[string]bool{}
	for _, g := range goals {
		ids[g.GoalID] = true
	}
	for _, g := range groups {
		if !ids[g.GoalID] || seen[g.GoalID] || normalized(g.Content.Name) == "" {
			return errors.New("unknown or duplicate card goalId")
		}
		seen[g.GoalID] = true
		flip, input := false, false
		for _, v := range g.Content.Cards {
			c, ok := storedCard(v)
			if !ok {
				return errors.New("invalid card")
			}
			key := string(c.Kind) + ":" + normalized(c.Question)
			if questions[key] {
				return errors.New("duplicate card")
			}
			questions[key] = true
			flip = flip || c.Kind == cards.KindFlip
			input = input || c.Kind == cards.KindInput
		}
		if !flip || !input {
			return errors.New("goal requires flip and input")
		}
	}
	return nil
}

// storedCard validates format-specific content and converts learning DTOs.
func storedCard(c Card) (cards.Card, bool) {
	var out cards.Card
	switch v := c.(type) {
	case FlipCard:
		out.Kind = cards.KindFlip
		out.Question = v.Question
		out.Answer = v.Answer
	case BooleanCard:
		out.Kind = cards.KindBoolean
		out.Question = v.Question
		out.Answer = strconv.FormatBool(v.Answer)
	case InputCard:
		out.Kind = cards.KindInput
		out.Question = v.Question
		out.Answer = v.Answer
	case ChoiceCard:
		out.Kind = cards.KindChoice
		out.Question = v.Question
		if len(v.RightAnswers) == 0 || len(v.WrongAnswers) == 0 {
			return out, false
		}
		seen := map[string]bool{}
		for _, a := range append(slices.Clone(v.RightAnswers), v.WrongAnswers...) {
			key := strings.ToLower(strings.TrimSpace(a))
			if key == "" || seen[key] {
				return out, false
			}
			seen[key] = true
		}
		out.RightAnswers = slices.Clone(v.RightAnswers)
		out.Options = append(slices.Clone(v.RightAnswers), v.WrongAnswers...)
		b, _ := json.Marshal(v.RightAnswers)
		out.Answer = string(b)
	case TableCard:
		out.Kind = cards.KindTable
		out.Question = v.Question
		out.Table = &cards.Table{}
		if len(v.Columns) < 2 {
			return out, false
		}
		names, rows := map[string]bool{}, map[string]bool{}
		for _, col := range v.Columns {
			name := strings.TrimSpace(col.Name)
			if name == "" || names[name] || len(col.Rows) == 0 {
				return out, false
			}
			names[name] = true
			out.Table.Columns = append(out.Table.Columns, name)
			for _, row := range col.Rows {
				row = strings.TrimSpace(row)
				if row == "" || rows[row] {
					return out, false
				}
				rows[row] = true
				out.Table.Items = append(out.Table.Items, cards.TableItem{Text: row, Column: name})
			}
		}
	default:
		return out, false
	}
	return out, strings.TrimSpace(out.Question) != "" && (out.Kind == cards.KindTable || strings.TrimSpace(out.Answer) != "")
}

// UnmarshalJSON checks required source fields before the server assigns goal IDs.
func (g *KnowledgeGoal) UnmarshalJSON(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for _, key := range []string{"goalId", "name", "topic", "pages", "source"} {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("missing goal field %s", key)
		}
	}
	type wire KnowledgeGoal
	return strictJSON(b, (*wire)(g))
}

// MarshalJSON makes the mandatory formats explicit in the stage contract.
func (g goalCards) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(g.Content)
	if err != nil {
		return nil, err
	}
	var f struct {
		Name  string            `json:"name"`
		Cards []json.RawMessage `json:"cards"`
	}
	if err = json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	var flip, input json.RawMessage
	extra := []json.RawMessage{}
	for _, raw := range f.Cards {
		var kind struct {
			Kind string `json:"kind"`
		}
		if err = json.Unmarshal(raw, &kind); err != nil {
			return nil, err
		}
		switch {
		case kind.Kind == "flip" && flip == nil:
			flip = raw
		case kind.Kind == "input" && input == nil:
			input = raw
		default:
			extra = append(extra, raw)
		}
	}
	return json.Marshal(struct {
		GoalID  string `json:"goalId"`
		Content any    `json:"content"`
	}{g.GoalID, struct {
		Name  string            `json:"name"`
		Flip  json.RawMessage   `json:"flip"`
		Input json.RawMessage   `json:"input"`
		Cards []json.RawMessage `json:"cards"`
	}{f.Name, flip, input, extra}})
}

func (g *goalCards) UnmarshalJSON(b []byte) error {
	var wire struct {
		GoalID  string `json:"goalId"`
		Content struct {
			Name  string            `json:"name"`
			Flip  json.RawMessage   `json:"flip"`
			Input json.RawMessage   `json:"input"`
			Cards []json.RawMessage `json:"cards"`
		} `json:"content"`
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	for kind, raw := range map[string]json.RawMessage{"flip": wire.Content.Flip, "input": wire.Content.Input} {
		var v struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &v) != nil || v.Kind != kind {
			return fmt.Errorf("goal requires explicit %s card", kind)
		}
	}
	if wire.Content.Cards == nil {
		return fmt.Errorf("missing additional cards array")
	}
	rawCards := append([]json.RawMessage{wire.Content.Flip, wire.Content.Input}, wire.Content.Cards...)
	f, err := json.Marshal(struct {
		Name  string            `json:"name"`
		Cards []json.RawMessage `json:"cards"`
	}{wire.Content.Name, rawCards})
	if err != nil {
		return err
	}
	g.GoalID = wire.GoalID
	return json.Unmarshal(f, &g.Content)
}
