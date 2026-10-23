package main

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func defaultProvider() string {
	if strings.TrimSpace(os.Getenv("OPENROUTER_KEY")) != "" {
		return "openrouter"
	}
	return "gigachat"
}

type modelUsage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	ReasoningTokens  *int    `json:"reasoningTokens,omitempty"`
	CostUSD          float64 `json:"costUsd"` // reported cost only; legacy providers do not report it
}

type runUsage struct {
	mu     sync.Mutex
	models map[string]modelUsage
}

type trackedProvider struct {
	providers.Provider
	usage *runUsage
}

func (p trackedProvider) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	res, err := p.Provider.Complete(ctx, req)
	if res.Cached {
		return res, err
	}
	p.usage.mu.Lock()
	defer p.usage.mu.Unlock()
	if p.usage.models == nil {
		p.usage.models = make(map[string]modelUsage)
	}
	u := p.usage.models[res.Model]
	u.Calls += max(1, res.Usage.Attempts)
	u.PromptTokens += res.Usage.PromptTokens
	u.CompletionTokens += res.Usage.CompletionTokens
	if res.Usage.ReasoningTokens != nil {
		if u.ReasoningTokens == nil {
			u.ReasoningTokens = new(int)
		}
		*u.ReasoningTokens += *res.Usage.ReasoningTokens
	}
	u.CostUSD += res.Usage.CostUSD
	p.usage.models[res.Model] = u
	return res, err
}

func (u *runUsage) snapshot() map[string]modelUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	result := make(map[string]modelUsage, len(u.models))
	for model, stats := range u.models {
		result[model] = stats
	}
	return result
}

func modelProfile(name string) providers.Profile {
	switch name {
	case "openrouter":
		return providers.OpenRouterProfile()
	case "qwen", "yandexgpt":
		return providers.QwenProfile()
	default:
		return providers.GigaChatProfile()
	}
}
func limitModel(p providers.Provider, name string) providers.Provider {
	return providers.Limit(p, modelProfile(name).Requests)
}

func processingLimits(provider string, explicit bool, usd float64, calls int) (providers.BudgetLimits, error) {
	if !explicit {
		usd = 0
		if provider == "openrouter" {
			usd = .15
		}
	}
	if usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) || calls < 1 {
		return providers.BudgetLimits{}, errors.New("budget must be finite and nonnegative, max-calls positive")
	}
	return providers.BudgetLimits{Calls: calls, USD: usd}, nil
}
