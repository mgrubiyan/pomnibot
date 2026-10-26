package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type illustrationBinding struct {
	CardID         string          `json:"cardId"`
	QuestionSvg    json.RawMessage `json:"questionSvg"`
	ExplanationSvg json.RawMessage `json:"explanationSvg"`
}

type cardPosition struct{ fact, card int }

// Each expanded recall is represented once. Its binding updates both formats,
// using the model path retained through the difficulty sort rather than text matching.
func illustrationInput(facts []Fact) ([]byte, map[string][]cardPosition) {
	type entry struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Card Card   `json:"card"`
	}
	entries := []entry{}
	bindings := map[string][]cardPosition{}
	for i, fact := range facts {
		paths := map[string]string{}
		for j, c := range fact.Cards {
			path := fact.cardPaths[j]
			id, found := paths[path]
			if !found {
				id = fmt.Sprintf("f%d-c%d", i, j)
				paths[path] = id
				entries = append(entries, entry{ID: id, Kind: []string{"recall", "boolean", "choice", "table", "recall"}[cardDifficulty(c)], Card: c})
			}
			bindings[id] = append(bindings[id], cardPosition{i, j})
		}
	}
	payload, _ := json.Marshal(struct {
		Cards []entry `json:"cards"`
	}{entries})
	return payload, bindings
}

func (g *Generator) enrichIllustrations(ctx context.Context, facts []Fact, out *Result) error {
	payload, bindings := illustrationInput(facts)
	s := map[string]any{"type": "string"}
	schema := schemaBytes(schemaObject(map[string]any{"illustrations": schemaArray(schemaObject(map[string]any{"cardId": s, "questionSvg": s, "explanationSvg": s}))}))
	req := providers.Request{System: prompts.GeneratorIllustrations, User: string(payload), Schema: schema, NoFallback: true, NoCache: true, ReasoningEffort: g.opts.ReasoningEffort, MaxTokens: g.opts.MaxTokens}
	req.Backend = out.Stats.Stages["generation"].Backend
	_ = g.runStage(ctx, "illustrations", req, out, func(resp providers.Response, st *StageStats) error {
		if resp.Truncated {
			return problem("truncated", "", "response was truncated")
		}
		if !json.Valid(resp.Content) {
			return problem("invalid_json", "", "invalid JSON")
		}
		if err := requiredFields(resp.Content, "illustrations"); err != nil {
			return err
		}
		var wire struct {
			Illustrations []json.RawMessage `json:"illustrations"`
		}
		if err := strictJSON(resp.Content, &wire); err != nil {
			return err
		}
		if wire.Illustrations == nil {
			return problem("null_field", "illustrations", "required field is null")
		}
		seen := map[string]bool{}
		for _, raw := range wire.Illustrations {
			var binding illustrationBinding
			if err := strictJSON(raw, &binding); err != nil || binding.CardID == "" {
				st.DroppedIllustrations++
				continue
			}
			positions, ok := bindings[binding.CardID]
			if !ok || seen[binding.CardID] {
				st.DroppedIllustrations++
				continue
			}
			seen[binding.CardID] = true
			question := decodeIllustration(binding.QuestionSvg, st)
			explanation := decodeIllustration(binding.ExplanationSvg, st)
			for _, pos := range positions {
				c := facts[pos.fact].Cards[pos.card]
				base := c.Base()
				base.QuestionSvg, base.ExplanationSvg = question, explanation
				if len(base.QuestionImageIDs) > 0 {
					base.QuestionSvg = ""
				}
				if len(base.ExplanationImageIDs) > 0 {
					base.ExplanationSvg = ""
				}
				facts[pos.fact].Cards[pos.card] = withCardBase(c, base)
			}
		}
		return nil
	})
	// Only cancellation of the whole job is fatal. A timeout of this optional
	// request, provider failure or exhausted budget leaves validated text usable.
	if ctx.Err() != nil {
		return contextualProblem(providerProblem(ctx.Err()), "illustrations", g.opts, out.Stats.Stages["illustrations"].Duration)
	}
	return nil
}

func decodeIllustration(raw json.RawMessage, st *StageStats) string {
	if len(raw) == 0 {
		return ""
	}
	var svg string
	if err := json.Unmarshal(raw, &svg); err != nil {
		st.DroppedIllustrations++
		return ""
	}
	if strings.TrimSpace(svg) == "" {
		return ""
	}
	if err := validateSVG(svg); err != nil {
		st.DroppedIllustrations++
		return ""
	}
	return svg
}

func withCardBase(c Card, base CardBase) Card {
	switch v := c.(type) {
	case FlipCard:
		v.CardBase = base
		return v
	case InputCard:
		v.CardBase = base
		return v
	case BooleanCard:
		v.CardBase = base
		return v
	case ChoiceCard:
		v.CardBase = base
		return v
	case TableCard:
		v.CardBase = base
		return v
	default:
		return c
	}
}
