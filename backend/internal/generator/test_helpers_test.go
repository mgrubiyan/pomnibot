package generator

import (
	"context"
	"encoding/json"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type providerFunc func(context.Context, providers.Request) (providers.Response, error)

func (p providerFunc) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return p(ctx, r)
}
func stageFixture(facts ...Fact) providers.Provider {
	return providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		b, e := json.Marshal(AIResponse{Facts: facts, Issues: []string{}})
		return providers.Response{Content: b, Model: "fixture"}, e
	})
}
func cardBase(q string) CardBase { return CardBase{Question: q, Source: "Source for " + q} }
func learningFact(name string) Fact {
	return Fact{Name: name, Pages: []int{1}, Cards: []Card{FlipCard{cardBase(name + "?"), "answer"}, InputCard{cardBase(name + "?"), "answer"}}}
}
