package generator

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// modelFact is the private generation contract. Persisted Fact keeps its cards wire format.
type modelFact struct {
	Name       string       `json:"name"`
	Pages      []int        `json:"pages"`
	Recall     recall       `json:"recall"`
	ExtraCards []modelExtra `json:"extraCards"`
}

type recall struct {
	CardBase
	Answer string `json:"answer"`
}

func (r *recall) UnmarshalJSON(b []byte) error {
	if err := requiredFields(b, "question", "answer", "source", "sourceQuote"); err != nil {
		return err
	}
	var wire struct {
		QuestionImageIDs    []string `json:"questionImageIds"`
		ExplanationImageIDs []string `json:"explanationImageIds"`
		NeedsSVG            bool     `json:"needsSvg"`
		Question            string   `json:"question"`
		Source              string   `json:"source"`
		SourceQuote         string   `json:"sourceQuote"`
		Answer              string   `json:"answer"`
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	*r = recall{CardBase: CardBase{needsSVG: wire.NeedsSVG, QuestionImageIDs: wire.QuestionImageIDs, ExplanationImageIDs: wire.ExplanationImageIDs, Question: wire.Question, Source: wire.Source, SourceQuote: wire.SourceQuote}, Answer: wire.Answer}
	return nil
}

type modelExtra struct{ card Card }

func (e *modelExtra) UnmarshalJSON(b []byte) error {
	if err := requiredFields(b, "kind"); err != nil {
		return err
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &header); err != nil {
		return safeDecodeProblem(err)
	}
	switch header.Kind {
	case "recall", "boolean", "choice", "table":
	default:
		return problem("invalid_structure", "kind", "unknown card kind")
	}
	if header.Kind == "recall" {
		// Decode the shared base with the same strict requirements as the primary recall.
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(b, &fields)
		delete(fields, "kind")
		content, _ := json.Marshal(fields)
		var r recall
		if err := json.Unmarshal(content, &r); err != nil {
			return err
		}
		e.card = FlipCard(r)
		return nil
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for key := range fields {
		if strings.EqualFold(key, "questionSvg") || strings.EqualFold(key, "explanationSvg") {
			return problem("invalid_structure", "<unknown>", "unknown field")
		}
	}
	var needsSVG bool
	if raw, ok := fields["needsSvg"]; ok {
		if err := json.Unmarshal(raw, &needsSVG); err != nil {
			return err
		}
		delete(fields, "needsSvg")
	}
	content, _ := json.Marshal(fields)
	c, err := decodeCard(content)
	if err == nil {
		base := c.Base()
		base.needsSVG = needsSVG
		c = withCardBase(c, base)
	}
	e.card = c
	return err
}

func (f *modelFact) UnmarshalJSON(b []byte) error {
	if err := requiredFields(b, "name", "pages", "recall", "extraCards"); err != nil {
		return err
	}
	// Decode nested values explicitly to preserve their original indices in diagnostics.
	var wire struct {
		Name       string            `json:"name"`
		Pages      []int             `json:"pages"`
		Recall     json.RawMessage   `json:"recall"`
		ExtraCards []json.RawMessage `json:"extraCards"`
	}
	if err := strictJSON(b, &wire); err != nil {
		return err
	}
	*f = modelFact{Name: wire.Name, Pages: wire.Pages, ExtraCards: []modelExtra{}}
	if err := json.Unmarshal(wire.Recall, &f.Recall); err != nil {
		return atPath("recall", err)
	}
	for i, raw := range wire.ExtraCards {
		var extra modelExtra
		if err := json.Unmarshal(raw, &extra); err != nil {
			return atPath(fmt.Sprintf("extraCards[%d]", i), err)
		}
		f.ExtraCards = append(f.ExtraCards, extra)
	}
	return nil
}

func (f modelFact) expand() Fact {
	out := Fact{Name: f.Name, Pages: f.Pages}
	add := func(c Card, path string) {
		out.Cards = append(out.Cards, c)
		out.cardPaths = append(out.cardPaths, path)
	}
	pair := func(c FlipCard, path string) {
		add(c, path)
		add(InputCard(c), path)
	}
	pair(FlipCard(f.Recall), "recall")
	for i, extra := range f.ExtraCards {
		path := fmt.Sprintf("extraCards[%d]", i)
		if c, ok := extra.card.(FlipCard); ok {
			pair(c, path)
		} else {
			add(extra.card, path)
		}
	}
	indices := make([]int, len(out.Cards))
	for i := range indices {
		indices[i] = i
	}
	slices.SortStableFunc(indices, func(a, b int) int { return cardDifficulty(out.Cards[a]) - cardDifficulty(out.Cards[b]) })
	cards, paths := slices.Clone(out.Cards), slices.Clone(out.cardPaths)
	for i, original := range indices {
		out.Cards[i] = cards[original]
		out.cardPaths[i] = paths[original]
	}
	return out
}
