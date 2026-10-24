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
func TestStructuredSourceAndWarningsReachGeneration(t *testing.T) {
	f := requiredFormatsFixture()
	p := stageFixture(f)
	calls := 0
	wrapped := providerFunc(func(ctx context.Context, r providers.Request) (providers.Response, error) {
		calls++
		if calls == 2 {
			return p.Complete(ctx, r)
		}
		if !strings.Contains(r.Source, "formula") || !strings.Contains(r.Source, "uncertain") || !r.NoFallback {
			t.Fatalf("lost source or fallback control: %+v", r)
		}
		return p.Complete(ctx, r)
	})
	r, e := NewGenerator(wrapped, Options{}).Generate(context.Background(), Document{Structure: &material.Document{Blocks: []material.Block{{ID: "p1", Page: 1, Text: "formula"}}, Pages: []material.Page{{Number: 1, Issues: []string{"uncertain"}}}}})
	if e != nil || calls != 2 || CardCount(r.Facts) != 5 {
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
