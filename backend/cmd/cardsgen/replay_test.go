package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

// TestLiveReplay is opt-in, billed evaluation. It explicitly reuses a private
// reading from this fixture, restoring source images locally without more OCR.
// Timings distinguish actual replay from a sum of separately measured stages.
func TestLiveReplay(t *testing.T) {
	if os.Getenv("POMNIBOT_LIVE_REPLAY") != "1" {
		t.Skip("billed evaluation requires explicit opt-in")
	}
	budget, err := strconv.ParseFloat(os.Getenv("POMNIBOT_EVAL_BUDGET"), 64)
	if err != nil || budget <= 0 || budget > .15 {
		t.Fatal("explicit experiment budget in (0,.15] required")
	}
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 80, USD: budget})
	ctx := providers.WithBudget(context.Background(), ledger)
	pdf := os.Getenv("POMNIBOT_EVAL_PDF")
	data, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if material.Digest(data) != "124e265eabf075590bbaef74b6cf98af64c99482f6340cc093686bcafc3b0198" {
		t.Fatal("SQL evaluation fixture checksum differs")
	}
	snapshot, err := os.ReadFile(os.Getenv("POMNIBOT_REPLAY_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Ingest         ingest.Result `json:"ingest"`
		ReadingSeconds float64       `json:"readingSeconds"`
	}
	if err = json.Unmarshal(snapshot, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.Ingest.Document.Version != material.Version || len(recorded.Ingest.Document.Pages) != 36 {
		t.Fatal("reading version/fixture differs")
	}
	dir := t.TempDir()
	prefix := filepath.Join(dir, "page")
	if err = exec.Command("pdftoppm", "-r", "150", "-jpeg", pdf, prefix).Run(); err != nil {
		t.Fatal(err)
	}
	for i := range recorded.Ingest.Document.Pages {
		p := &recorded.Ingest.Document.Pages[i]
		p.Image, err = os.ReadFile(fmt.Sprintf("%s-%02d.jpg", prefix, p.Number))
		if err != nil {
			t.Fatal(err)
		}
		p.ImageMime = "image/jpeg"
	}
	cfg, err := openrouter.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := openrouter.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	usage := &runUsage{}
	writer := providers.Limit(trackedProvider{Provider: provider, usage: usage}, 1)
	began := time.Now()
	first := 0.0
	gen := generator.NewGenerator(writer, generator.Options{Concurrency: 1, OnBatch: func(b generator.Batch) {
		if first == 0 {
			first = time.Since(began).Seconds()
		}
		t.Logf("reviewed cards +%d at %.1fs", len(b.Cards), time.Since(began).Seconds())
	}})
	result, genErr := gen.Generate(ctx, generator.Document{Title: "Введение в SQL", Structure: &recorded.Ingest.Document})
	out := struct {
		generator.Result
		ModelUsage                 map[string]modelUsage `json:"modelUsage"`
		ReadingSeconds             float64               `json:"reusedReadingSeconds"`
		ReplaySeconds              float64               `json:"replaySeconds"`
		FirstVerifiedReplaySeconds float64               `json:"firstVerifiedReplaySeconds"`
		Error                      string                `json:"error,omitempty"`
	}{Result: result, ModelUsage: usage.snapshot(), ReadingSeconds: recorded.ReadingSeconds, ReplaySeconds: time.Since(began).Seconds(), FirstVerifiedReplaySeconds: first}
	if genErr != nil {
		out.Error = genErr.Error()
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("POMNIBOT_REPLAY_OUTPUT"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if genErr != nil {
		t.Fatal(genErr)
	}
}
