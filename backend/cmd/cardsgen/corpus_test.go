package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

// TestLiveCorpus sends only the explicitly selected pages. All other pages
// receive local inspection, without claiming full model/card coverage.
func TestLiveCorpus(t *testing.T) {
	if os.Getenv("POMNIBOT_LIVE_CORPUS") != "1" {
		t.Skip("billed corpus evaluation requires explicit opt-in")
	}
	maxUSD, budgetErr := strconv.ParseFloat(os.Getenv("POMNIBOT_CORPUS_DOCUMENT_BUDGET"), 64)
	if budgetErr != nil || maxUSD <= 0 || maxUSD > .02 {
		t.Fatal("explicit per-document experiment budget in (0,.02] required")
	}
	cfg, err := openrouter.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	client, err := openrouter.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	type evaluated struct {
		Name          string `json:"name"`
		SHA256        string `json:"sha256"`
		TotalPages    int    `json:"totalPages"`
		SelectedPages []int  `json:"selectedPages"`
		generator.Result
		Usage                map[string]modelUsage `json:"modelUsage"`
		ElapsedSeconds       float64               `json:"elapsedSeconds"`
		FirstVerifiedSeconds float64               `json:"firstVerifiedSeconds"`
		Error                string                `json:"error,omitempty"`
	}
	var output []evaluated
	// Documents and provider requests are deliberately sequential in billed evaluation.
	for _, fixture := range []struct {
		name  string
		pages []int
	}{
		{"БД_Сем5_Лекция2.pdf", []int{11, 12, 14, 15}},
		{"БД_Сем5_Лекция4_SELECT_великий_и_ужасный (2).pdf", []int{9, 10, 37, 39}},
		{"БД_Сем5_Лекция10_Индексирование_БД.pdf", []int{4, 11, 16, 24}},
	} {
		began := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		ledger := providers.NewBudget(providers.BudgetLimits{Calls: 20, USD: maxUSD})
		ctx = providers.WithBudget(ctx, ledger)
		usage := &runUsage{}
		p := providers.Limit(evaluationRecorder{Provider: trackedProvider{Provider: client, usage: usage}, Path: os.Getenv("POMNIBOT_CORPUS_OUTPUT") + ".requests.jsonl"}, 1)
		data, err := os.ReadFile(filepath.Join(os.Getenv("POMNIBOT_CORPUS_ROOT"), fixture.name))
		if err != nil {
			t.Fatal(err)
		}
		ex, err := ingest.NewExtractor(nil, ingest.Options{AssessPDF: true})
		if err != nil {
			t.Fatal(err)
		}
		local, err := ex.Extract(ctx, []ingest.File{{Name: fixture.name, Data: data}})
		if err != nil {
			t.Fatal(err)
		}
		layers := make([]string, len(local.Pages))
		for i, page := range local.Document.Pages {
			layers[i] = page.Reading
		}
		only := make([]int, len(fixture.pages))
		for i, n := range fixture.pages {
			only[i] = n - 1
		}
		got, err := (&ingest.VisionOCR{Model: p, FullPage: true, Concurrency: 1}).Recognize(ctx, ingest.OCRRequest{Data: data, MimeType: "application/pdf", Pages: len(local.Pages), Only: only, TextLayers: layers, Model: ingest.ModelScan})
		if err != nil {
			t.Fatal(err)
		}
		d := material.Document{Version: material.Version, Digest: material.Digest(data)}
		for _, n := range fixture.pages {
			read := got[n-1]
			d.Pages = append(d.Pages, material.Page{Number: n, OriginalPage: n, TextLayer: layers[n-1], Reading: read.Text, Observations: read.Observations, Issues: read.Issues, Image: read.Image, ImageMime: read.ImageMime})
			blocks := read.Blocks
			if len(blocks) == 0 {
				blocks = []material.Block{{Kind: "text", Text: read.Text}}
			}
			for i, b := range blocks {
				b.ID = fmt.Sprintf("p%d-b%d", n, i+1)
				b.Page = n
				b.Order = i
				if len(b.Issues) == 0 {
					b.Issues = read.Issues
				}
				d.Blocks = append(d.Blocks, b)
			}
		}
		first := 0.0
		gen := generator.NewGenerator(p, generator.Options{Concurrency: 1, OnBatch: func(b generator.Batch) {
			if first == 0 {
				first = time.Since(began).Seconds()
			}
			t.Logf("%s: reviewed +%d", fixture.name, len(b.Cards))
		}})
		result, genErr := gen.Generate(ctx, generator.Document{Title: fixture.name, Structure: &d})
		cancel()
		result.Budget = ledger.Snapshot()
		result.Artifact = &generator.Artifact{Version: generator.PipelineVersion, Reading: d}
		record := evaluated{Name: fixture.name, SHA256: material.Digest(data), TotalPages: len(local.Pages), SelectedPages: fixture.pages, Result: result, Usage: usage.snapshot(), ElapsedSeconds: time.Since(began).Seconds(), FirstVerifiedSeconds: first}
		if genErr != nil {
			record.Error = genErr.Error()
		}
		output = append(output, record)
		encoded, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(os.Getenv("POMNIBOT_CORPUS_OUTPUT"), encoded, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: local pages=%d selected=%v cards=%d elapsed=%.1fs", fixture.name, len(local.Pages), fixture.pages, len(result.Cards), record.ElapsedSeconds)
	}
}
