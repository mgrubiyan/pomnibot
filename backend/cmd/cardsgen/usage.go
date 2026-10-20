package main

import (
	"context"
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
	if err != nil {
		return res, err
	}
	p.usage.mu.Lock()
	defer p.usage.mu.Unlock()
	if p.usage.models == nil {
		p.usage.models = make(map[string]modelUsage)
	}
	u := p.usage.models[res.Model]
	u.Calls++
	u.PromptTokens += res.Usage.PromptTokens
	u.CompletionTokens += res.Usage.CompletionTokens
	u.CostUSD += res.Usage.CostUSD
	p.usage.models[res.Model] = u
	return res, nil
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
