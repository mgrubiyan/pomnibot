package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
type illustrationEntry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Card Card   `json:"card"`
}

func illustrationInput(facts []Fact, selected []figureCandidate) ([]illustrationEntry, []illustrationEntry, map[string][]cardPosition) {
	svgCards := []illustrationEntry{}
	figureCards := []illustrationEntry{}
	bindings := map[string][]cardPosition{}
	selectedIDs := map[string]bool{}
	for _, f := range selected {
		selectedIDs[f.ID] = true
	}
	for i, fact := range facts {
		paths := map[string]string{}
		for j, c := range fact.Cards {
			path := fact.cardPaths[j]
			id, found := paths[path]
			if !found {
				id = fmt.Sprintf("f%d-c%d", i, j)
				paths[path] = id
				entry := illustrationEntry{ID: id, Kind: []string{"recall", "boolean", "choice", "table", "recall"}[cardDifficulty(c)], Card: c}
				if c.Base().needsSVG {
					svgCards = append(svgCards, entry)
				}
				usesFigure := false
				for _, ids := range [][]string{c.Base().QuestionImageIDs, c.Base().ExplanationImageIDs} {
					for _, ref := range ids {
						usesFigure = usesFigure || selectedIDs[ref]
					}
				}
				if usesFigure {
					figureCards = append(figureCards, entry)
				}
			}
			if c.Base().needsSVG {
				bindings[id] = append(bindings[id], cardPosition{i, j})
			}
		}
	}
	return svgCards, figureCards, bindings
}

func illustrationSchema() json.RawMessage {
	s := map[string]any{"type": "string"}
	// Provider-compatible subset: lengths and coordinate ranges are checked locally.
	return schemaBytes(schemaObject(map[string]any{
		"regions":       schemaArray(schemaObject(map[string]any{"id": s, "bounds": schemaArray(map[string]any{"type": "number"})})),
		"illustrations": schemaArray(schemaObject(map[string]any{"cardId": s, "questionSvg": s, "explanationSvg": s})),
	}))
}

func (g *Generator) prepareIllustrations(ctx context.Context, doc Document, reply *AIResponse, out *Result) error {
	began := time.Now()
	defer func() { out.Stats.ImageDuration = time.Since(began) }()
	out.Stats.ImagesFound = len(reply.Figures)
	prepared := g.selectFigurePages(ctx, doc, *reply, out)
	cards, figureCards, bindings := illustrationInput(reply.Facts, prepared.Figures)
	resolved := map[string]string{}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(prepared.Figures) > 0 || len(cards) > 0 {
		payload := schemaBytes(map[string]any{"pages": prepared.Pages, "figures": prepared.Figures, "cards": cards, "figureCards": figureCards})
		req := providers.Request{System: prompts.GeneratorIllustrations, User: string(payload), Images: prepared.Images, Schema: illustrationSchema(), NoFallback: true, NoRetry: true, NoCache: true, ReasoningEffort: g.opts.ReasoningEffort, MaxTokens: g.opts.MaxTokens, Backend: out.Stats.Stages["generation"].Backend}
		err := g.runStage(ctx, "illustrations", req, out, func(resp providers.Response, st *StageStats) error {
			if resp.Truncated {
				return problem("truncated", "", "response was truncated")
			}
			if !json.Valid(resp.Content) {
				return problem("invalid_json", "", "invalid JSON")
			}
			if err := requiredFields(resp.Content, "regions", "illustrations"); err != nil {
				return err
			}
			var wire struct {
				Regions       []json.RawMessage `json:"regions"`
				Illustrations []json.RawMessage `json:"illustrations"`
			}
			if err := strictJSON(resp.Content, &wire); err != nil {
				return err
			}
			resolved = extractSelectedRegions(wire.Regions, prepared, doc, out)
			applyIllustrations(wire.Illustrations, reply.Facts, bindings, st)
			return nil
		})
		if err != nil {
			out.imageIssue("illustration preparation failed: " + err.Error())
		}
	}
	// An optional-stage timeout preserves usable cards; cancellation of the whole
	// job must never publish even a partial result.
	if ctx.Err() != nil {
		return contextualProblem(providerProblem(ctx.Err()), "illustrations", g.opts, time.Since(began))
	}
	facts, err := resolveFigureBindings(reply.Facts, resolved, out, true)
	if err != nil {
		return contextualProblem(err, "illustrations", g.opts, time.Since(began))
	}
	reply.Facts = facts
	return validateImageBindings(facts, out.Images)
}

func applyIllustrations(raws []json.RawMessage, facts []Fact, bindings map[string][]cardPosition, st *StageStats) {
	seen := map[string]bool{}
	for _, raw := range raws {
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
