package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type providerFunc func(context.Context, providers.Request) (providers.Response, error)

func (f providerFunc) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	return f(ctx, req)
}

func TestRunUsageIncludesReadingAndGeneration(t *testing.T) {
	usage := &runUsage{}
	p := trackedProvider{Provider: providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Model: "haiku", Usage: providers.Usage{PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.001}}, nil
	}), usage: usage}
	for _, req := range []providers.Request{{Image: &providers.Image{}}, {User: "notes"}} {
		if _, err := p.Complete(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	u := usage.snapshot()["haiku"]
	if u.Calls != 2 || u.PromptTokens != 20 || u.CompletionTokens != 10 || u.CostUSD != 0.002 {
		t.Fatalf("usage=%+v", u)
	}
	p.Provider = providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("offline")
	})
	if _, err := p.Complete(context.Background(), providers.Request{}); err == nil {
		t.Fatal("expected error")
	}
	if usage.snapshot()["haiku"].Calls != 2 {
		t.Fatal("failed call counted as successful")
	}
}

func TestDefaultProvider(t *testing.T) {
	for _, tc := range []struct{ key, want string }{{"", "gigachat"}, {"  ", "gigachat"}, {"secret", "openrouter"}} {
		t.Setenv("OPENROUTER_KEY", tc.key)
		if got := defaultProvider(); got != tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
}
