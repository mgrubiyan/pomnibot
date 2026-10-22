// Package generator makes cards with one whole-document request.
package generator

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// PipelineVersion identifies source input, prompt and code validation semantics.
const PipelineVersion = "whole-source-learning-goals-v5"

// Generator is safe for concurrent use. The provider owns transport retries.
type Generator struct {
	provider providers.Provider
	opts     Options
}

// NewGenerator returns the single document generator.
func NewGenerator(p providers.Provider, opts Options) *Generator {
	return &Generator{provider: p, opts: opts.withDefaults()}
}

// WithOnBatch returns a copy with a caller-specific progress callback.
func (g *Generator) WithOnBatch(cb func(Batch)) *Generator {
	if g == nil {
		return nil
	}
	cp := *g
	cp.opts.OnBatch = cb
	return &cp
}

// Generate uses the same request and checks for structured readings and text.
func (g *Generator) Generate(ctx context.Context, doc Document) (out Result, err error) {
	if providers.BudgetFrom(ctx) == nil {
		ctx = providers.WithBudget(ctx, providers.NewBudget(g.opts.Budget))
	}
	defer func() { out.Budget = providers.BudgetFrom(ctx).Snapshot() }()
	if doc.Structure == nil {
		if strings.TrimSpace(doc.Text) == "" {
			return out, fmt.Errorf("empty document")
		}
		d := material.Document{}
		starts := doc.PageStarts
		if len(starts) == 0 {
			d.Blocks = []material.Block{{ID: "text", Kind: "text", Text: doc.Text}}
		} else {
			previous := -1
			for _, start := range starts {
				if start < 0 || start > len(doc.Text) || start <= previous {
					return out, fmt.Errorf("invalid page offsets")
				}
				previous = start
			}
			if starts[0] != 0 {
				return out, fmt.Errorf("page offsets omit source prefix")
			}
			for i, start := range starts {
				end := len(doc.Text)
				if i+1 < len(starts) {
					end = starts[i+1]
				}
				d.Blocks = append(d.Blocks, material.Block{ID: fmt.Sprintf("p%d", i+1), Page: i + 1, Kind: "text", Text: doc.Text[start:end]})
			}
		}
		doc.Structure = &d
	}
	return g.generateDirect(ctx, doc)
}

func (g *Generator) notify(b Batch) {
	if g.opts.OnBatch == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("cards: OnBatch panicked", "panic", p)
		}
	}()
	g.opts.OnBatch(Batch{Cards: slices.Clone(b.Cards), Facts: slices.Clone(b.Facts)})
}

func factID(docID, key string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(docID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("%016x", h.Sum64())
}

func evidenceRef(title string, d material.Document, es []material.Evidence) string {
	var pages []int
	for _, e := range es {
		if b, ok := d.Block(e.BlockID); ok && b.Page > 0 && !slices.Contains(pages, b.Page) {
			pages = append(pages, b.Page)
		}
	}
	if len(pages) == 0 {
		return title
	}
	slices.Sort(pages)
	refs := make([]string, len(pages))
	for i, p := range pages {
		refs[i] = fmt.Sprint(p)
	}
	return title + ", стр. " + strings.Join(refs, ", ")
}
