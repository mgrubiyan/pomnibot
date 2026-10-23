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
func stageFixture(goals []KnowledgeGoal, groups []goalCards) providers.Provider {
	calls := 0
	return providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		r := stageResponse{Issues: []string{}, Goals: []KnowledgeGoal{}, Cards: []goalCards{}, Additions: []KnowledgeGoal{}, Complete: new(false)}
		switch calls {
		case 1:
			r.Goals = goals
		case 2:
			r.Cards = groups
		case 3:
			r.Goals = goals
			r.Cards = groups
			r.Complete = new(true)
		}
		b, e := json.Marshal(r)
		return providers.Response{Content: b, Model: "fixture"}, e
	})
}
func learningGoal(id, name string) KnowledgeGoal {
	return KnowledgeGoal{GoalID: id, Name: name, Topic: "T", Pages: []int{1}, Source: "A and B"}
}
