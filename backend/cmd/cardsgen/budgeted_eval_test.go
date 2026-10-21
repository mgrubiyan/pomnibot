package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/processingcache"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

// Opt-in experiment: no paid API is contacted by ordinary task test. It uses
// an existing private reading and checksum-matched local source images.
func TestLiveBudgetedEvaluation(t *testing.T) {
	if os.Getenv("POMNIBOT_LIVE_BUDGETED") != "1" {
		t.Skip("explicit opt-in required for paid experiment")
	}
	maxUSD, err := strconv.ParseFloat(os.Getenv("POMNIBOT_EVAL_BUDGET"), 64)
	if err != nil || maxUSD <= 0 || maxUSD > .15 {
		t.Fatal("explicit budget in (0,.15] required")
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
	pdf, err := os.ReadFile(os.Getenv("POMNIBOT_EVAL_PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if material.Digest(pdf) != "124e265eabf075590bbaef74b6cf98af64c99482f6340cc093686bcafc3b0198" {
		t.Fatal("fixture checksum differs")
	}
	d := recorded.Ingest.Document
	selected := map[int]bool{}
	for _, n := range strings.Split(os.Getenv("POMNIBOT_EVAL_PAGES"), ",") {
		if n != "" {
			page, e := strconv.Atoi(n)
			if e != nil {
				t.Fatal(e)
			}
			selected[page] = true
		}
	}
	var pages []material.Page
	for _, p := range d.Pages {
		if len(selected) > 0 && !selected[p.Number] {
			continue
		}
		// Evaluation-only rendering directory corresponds to the checksum above.
		imagePath := filepath.Join(os.Getenv("POMNIBOT_EVAL_IMAGES"), fmt.Sprintf("slide-%02d.jpg", p.Number))
		p.Image, err = os.ReadFile(imagePath)
		if err != nil {
			t.Fatal(err)
		}
		p.ImageMime = "image/jpeg"
		pages = append(pages, p)
	}
	d.Pages = pages
	var blocks []material.Block
	for _, b := range d.Blocks {
		if len(selected) == 0 || selected[b.Page] {
			blocks = append(blocks, b)
		}
	}
	d.Blocks = blocks
	cfg, err := openrouter.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	client, err := openrouter.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	usage := &runUsage{}
	var source providers.Provider = client
	if path := os.Getenv("POMNIBOT_RESUME_TRACE"); path != "" {
		replay, loadErr := loadEvaluationReplay(path, client)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		source = replay
	}
	p := providers.Limit(evaluationRecorder{Provider: trackedProvider{Provider: source, usage: usage}, Path: os.Getenv("POMNIBOT_REPLAY_OUTPUT") + ".requests.jsonl"}, 1)
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 80, USD: maxUSD})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = providers.WithBudget(ctx, ledger)
	began := time.Now()
	first := 0.0
	gen := generator.NewGenerator(p, generator.Options{Concurrency: 1, OnBatch: func(b generator.Batch) {
		if first == 0 {
			first = time.Since(began).Seconds()
		}
		t.Logf("reviewed +%d at %.1fs", len(b.Cards), time.Since(began).Seconds())
	}})
	result, genErr := gen.Generate(ctx, generator.Document{Title: "Введение в SQL", Structure: &d})
	out := struct {
		generator.Result
		ModelUsage                 map[string]modelUsage    `json:"modelUsage"`
		Budget                     providers.BudgetSnapshot `json:"budget"`
		ReplaySeconds              float64                  `json:"replaySeconds"`
		FirstVerifiedReplaySeconds float64                  `json:"firstVerifiedReplaySeconds"`
		ReusedReadingSeconds       float64                  `json:"reusedReadingSeconds"`
		SelectedPages              map[int]bool             `json:"selectedPages,omitempty"`
		Error                      string                   `json:"error,omitempty"`
	}{Result: result, ModelUsage: usage.snapshot(), Budget: ledger.Snapshot(), ReplaySeconds: time.Since(began).Seconds(), FirstVerifiedReplaySeconds: first, ReusedReadingSeconds: recorded.ReadingSeconds, SelectedPages: selected}
	if genErr != nil {
		out.Error = genErr.Error()
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("POMNIBOT_REPLAY_OUTPUT"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("cards=%d covered=%d missing=%d calls=%d reported=$%.6f committed=$%.6f", len(result.Cards), len(result.Coverage.Covered), len(result.Coverage.Missing), out.Budget.Calls, out.Budget.ReportedUSD, out.Budget.CommittedUSD)
	if genErr != nil {
		t.Fatal(genErr)
	}
}

// Private trace permits exact local replay of malformed paid responses. It
// omits image bytes and credentials; prompts and outputs stay outside git.
type evaluationRecorder struct {
	providers.Provider
	Path string
}

func (p evaluationRecorder) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	response, err := p.Provider.Complete(ctx, r)
	count := len(providers.RequestImages(r))
	r.Image = nil
	r.Images = nil
	record := struct {
		Request    providers.Request
		Response   providers.Response
		ImageCount int
		Error      string
		Budget     *providers.BudgetSnapshot `json:"budget,omitempty"`
	}{Request: r, Response: response, ImageCount: count}
	if err != nil {
		record.Error = err.Error()
	}
	if ledger := providers.BudgetFrom(ctx); ledger != nil {
		snapshot := ledger.Snapshot()
		record.Budget = &snapshot
	}
	data, _ := json.Marshal(record)
	f, e := os.OpenFile(p.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return response, e
	}
	if e = f.Chmod(0600); e != nil {
		_ = f.Close()
		return response, e
	}
	_, e = f.Write(append(data, '\n'))
	_ = f.Close()
	if e != nil {
		return response, e
	}
	return response, err
}

// Explicit evaluation replay is not a production success cache. Every recorded
// response is revalidated by the processing stage; failed requests are omitted.
type evaluationReplay struct {
	providers.Provider
	Records map[string]providers.Response
}

func loadEvaluationReplay(path string, p providers.Provider) (evaluationReplay, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evaluationReplay{}, err
	}
	out := evaluationReplay{Provider: p, Records: map[string]providers.Response{}}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var record struct {
			Request  providers.Request
			Response providers.Response
			Error    string
		}
		if err = json.Unmarshal([]byte(line), &record); err != nil {
			return out, err
		}
		if record.Error == "" && !record.Response.Truncated && strings.HasPrefix(record.Request.System, "STAGE:PLAN") {
			out.Records[processingcache.Key(record.Request)] = record.Response
		}
	}
	return out, nil
}
func (p evaluationReplay) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	if err := ctx.Err(); err != nil {
		return providers.Response{}, err
	}
	if response, ok := p.Records[processingcache.Key(r)]; ok {
		response.Cached = true
		response.Usage = providers.Usage{}
		return response, nil
	}
	return p.Provider.Complete(ctx, r)
}
