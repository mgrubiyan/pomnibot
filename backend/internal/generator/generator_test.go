package generator

import (
	"context"
	"errors"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"strings"
	"testing"
)

func TestCompleteSourceLimitBeforeBilling(t *testing.T) {
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{}, nil
	})
	_, e := NewGenerator(p, Options{SourceBudget: 10}).Generate(context.Background(), Document{Text: strings.Repeat("x", 100)})
	if !errors.Is(e, providers.ErrCapability) || calls != 0 {
		t.Fatalf("%v calls %d", e, calls)
	}
}
func TestStructuredSourceAndWarningsReachEveryStage(t *testing.T) {
	goals := []KnowledgeGoal{learningGoal("g1", "F")}
	f := requiredFormatsFixture()
	p := stageFixture(goals, []goalCards{{GoalID: "g1", Content: f}})
	calls := 0
	wrapped := providerFunc(func(ctx context.Context, r providers.Request) (providers.Response, error) {
		calls++
		if !strings.Contains(r.Source, "formula") || !strings.Contains(r.Source, "uncertain") || !r.NoFallback {
			t.Fatalf("lost source or fallback control: %+v", r)
		}
		return p.Complete(ctx, r)
	})
	r, e := NewGenerator(wrapped, Options{}).Generate(context.Background(), Document{Structure: &material.Document{Blocks: []material.Block{{ID: "p1", Page: 1, Text: "formula"}}, Pages: []material.Page{{Number: 1, Issues: []string{"uncertain"}}}}})
	if e != nil || calls != 3 || len(r.Cards) != 5 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestGoalRequiresSourceFieldsBeforeServerIDAssignment(t *testing.T) {
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(`{"issues":[],"complete":false,"cards":[],"additions":[],"goals":[{"name":"A","topic":"T","pages":[1],"source":"A"}]}`)}, nil
	})
	r, e := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "A"})
	if e == nil || r.Stats.InvalidResponses != 1 || len(r.Cards) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestSourcePageOffsetsAreValidatedBeforeCallingModel(t *testing.T) {
	for _, starts := range [][]int{{2}, {0, 0}, {0, 100}, {-1}} {
		calls := 0
		p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
			calls++
			return providers.Response{}, nil
		})
		_, e := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "page", PageStarts: starts})
		if e == nil || calls != 0 {
			t.Fatalf("offsets %v: %v calls %d", starts, e, calls)
		}
	}
}
