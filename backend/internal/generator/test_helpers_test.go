package generator

import (
	"context"
	"encoding/json"
	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type providerFunc func(context.Context, providers.Request) (providers.Response, error)

func (p providerFunc) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return p(ctx, r)
}
func stageFixture(facts ...Fact) providers.Provider {
	return providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		if req.System == prompts.GeneratorIllustrations {
			return providers.Response{Content: []byte(`{"regions":[],"illustrations":[]}`), Model: "fixture"}, nil
		}
		b, e := marshalModelFixture(facts...)
		return providers.Response{Content: b, Model: "fixture"}, e
	})
}
func cardBase(q string) CardBase { return CardBase{Question: q, Source: "Source for " + q} }
func learningFact(name string) Fact {
	return Fact{Name: name, Pages: []int{1}, Cards: []Card{FlipCard{cardBase(name + "?"), "answer"}, InputCard{cardBase(name + "?"), "answer"}}}
}

// marshalModelFixture adapts stored-card fixtures to the private model wire contract.
func marshalModelFixture(facts ...Fact) ([]byte, error) {
	wire := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		var primary any
		extras := []any{}
		for _, c := range f.Cards {
			if _, ok := c.(InputCard); ok {
				continue
			}
			b, _ := json.Marshal(c)
			var fields map[string]any
			_ = json.Unmarshal(b, &fields)
			fields["needsSvg"] = true
			if _, ok := c.(FlipCard); ok && primary == nil {
				primary = fields
				continue
			}
			kind := []string{"recall", "boolean", "choice", "table", "input"}[cardDifficulty(c)]
			fields["kind"] = kind
			extras = append(extras, fields)
		}
		wire = append(wire, map[string]any{"name": f.Name, "pages": f.Pages, "recall": primary, "extraCards": extras})
	}
	return json.Marshal(map[string]any{"facts": wire, "issues": []string{}, "figures": []any{}})
}
