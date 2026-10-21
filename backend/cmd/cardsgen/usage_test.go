package main

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestConfiguredProviderKeepsItsOwnRequestLimit(t *testing.T) {
	for _, name := range []string{"gigachat", "openrouter"} {
		t.Run(name, func(t *testing.T) {
			var current, peak atomic.Int32
			p := limitModel(providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				n := current.Add(1)
				defer current.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				return providers.Response{}, nil
			}), name)
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() { _, _ = p.Complete(context.Background(), providers.Request{}) })
			}
			wg.Wait()
			if name == "gigachat" && peak.Load() != 1 || name == "openrouter" && peak.Load() < 2 {
				t.Fatalf("wrong provider limit: %s peak=%d", name, peak.Load())
			}
		})
	}
}

func TestDefaultDollarBudgetDoesNotDisableUnpricedProviders(t *testing.T) {
	for _, provider := range []string{"gigachat", "qwen", "yandexgpt", "openrouter"} {
		limits, err := processingLimits(provider, false, .15, 96)
		if err != nil {
			t.Fatal(err)
		}
		want := 0.
		if provider == "openrouter" {
			want = .15
		}
		if limits.USD != want || limits.Calls != 96 {
			t.Fatalf("wrong automatic limits for %s: %+v", provider, limits)
		}
		limits, err = processingLimits(provider, true, .07, 32)
		if err != nil || limits.USD != .07 || limits.Calls != 32 {
			t.Fatalf("explicit user budget lost: %+v %v", limits, err)
		}
	}
	if _, err := processingLimits("openrouter", true, math.NaN(), 96); err == nil {
		t.Fatal("NaN accepted")
	}
}
