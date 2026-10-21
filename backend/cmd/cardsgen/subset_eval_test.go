package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

// This journal belongs to the opt-in evaluation, not the application cache.
// Each event reaches disk before the next paid operation can begin.
type subsetJournal struct{ dir string }

func (j subsetJournal) save(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(j.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
func (j subsetJournal) event(kind string, value any) error {
	data, err := json.Marshal(map[string]any{"atUTC": time.Now().UTC(), "kind": kind, "value": value})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(j.dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

type subsetProvider struct {
	providers.Provider
	journal subsetJournal
	next    int
}

func subsetBound(r providers.Request) float64 {
	n := len(r.System) + len(r.User) + len(r.Schema) + 2048
	for _, im := range providers.RequestImages(r) {
		dims, _, err := image.DecodeConfig(bytes.NewReader(im.Data))
		if err != nil {
			n += max(32768, len(im.Data))
		} else {
			n += max(4096, (dims.Width*dims.Height+127)/128)
		}
	}
	return float64(n)*.1/1e6 + 8000*.5/1e6
}
func (p *subsetProvider) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	p.next++
	id := p.next
	bound := subsetBound(r)
	var images []map[string]any
	for i, im := range providers.RequestImages(r) {
		name := fmt.Sprintf("request-%02d-image-%02d.json", id, i)
		if err := p.journal.save(name, im); err != nil {
			return providers.Response{}, err
		}
		images = append(images, map[string]any{"path": name, "sha256": material.Digest(im.Data), "mime": im.MimeType})
	}
	saved := r
	saved.Image = nil
	saved.Images = nil
	if err := p.journal.save(fmt.Sprintf("request-%02d.json", id), map[string]any{"request": saved, "images": images, "boundUSD": bound}); err != nil {
		return providers.Response{}, err
	}
	if bound > .008+1e-12 {
		return providers.Response{}, errors.Join(providers.ErrBudget, p.journal.event("request-blocked-by-bound", map[string]any{"id": id, "boundUSD": bound}))
	}
	if err := p.journal.event("logical-start", map[string]any{"id": id, "boundUSD": bound}); err != nil {
		return providers.Response{}, err
	}
	began := time.Now()
	res, err := p.Provider.Complete(ctx, r)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	var ledger any
	if b := providers.BudgetFrom(ctx); b != nil {
		ledger = b.Snapshot()
	}
	saveErr := p.journal.save(fmt.Sprintf("response-%02d.json", id), map[string]any{"response": res, "error": errText, "seconds": time.Since(began).Seconds(), "budget": ledger})
	return res, errors.Join(err, saveErr)
}

type subsetTransport struct {
	base     http.RoundTripper
	journal  subsetJournal
	attempts int
}

func (tr *subsetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Production ReserveCall has already reserved this exact attempt.
	tr.attempts++
	if tr.attempts > 7 {
		return nil, providers.ErrBudget
	}
	if err := tr.journal.event("transport-start", map[string]any{"attempt": tr.attempts}); err != nil {
		return nil, err
	}
	began := time.Now()
	res, err := tr.base.RoundTrip(r)
	if err != nil {
		// Transport error strings may contain private URLs. Persist only the class.
		_ = tr.journal.event("transport-error", map[string]any{"attempt": tr.attempts, "seconds": time.Since(began).Seconds(), "error": "network or cancellation; charge unknown"})
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(data))
	var meta struct {
		Model string          `json:"model"`
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(data, &meta)
	if e := tr.journal.event("transport-response", map[string]any{"attempt": tr.attempts, "status": res.StatusCode, "model": meta.Model, "usage": meta.Usage, "seconds": time.Since(began).Seconds(), "readFailed": readErr != nil}); e != nil {
		return nil, e
	}
	if readErr != nil {
		return nil, errors.New("response read failed")
	}
	return res, nil
}

// TestLiveSQLSubset is a single sequential end-to-end experiment on the original
// PDF. Defaults cannot contact a model; fixed limits retain the authorized margin.
func TestLiveSQLSubset(t *testing.T) {
	if os.Getenv("POMNIBOT_LIVE_SQL_SUBSET") != "1" {
		t.Skip("explicit opt-in required for paid SQL subset")
	}
	dir := os.Getenv("POMNIBOT_SQL_SUBSET_DIR")
	if dir == "" {
		t.Fatal("private journal directory required")
	}
	j := subsetJournal{dir}
	if _, err := os.Stat(filepath.Join(dir, "preflight.json")); err != nil {
		t.Fatal("saved preflight required")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// An exclusive marker also prevents accidental paid repetition after a crash.
	must(j.save("paid-run-started.json", map[string]any{"atUTC": time.Now().UTC()}))
	cfg, err := openrouter.ConfigFromEnv()
	must(err)
	if cfg.Model != openrouter.DefaultModel || cfg.MaxTokens != 8000 || cfg.BaseURL != openrouter.DefaultBaseURL {
		t.Fatal("configuration differs from preflight")
	}
	tr := &subsetTransport{base: http.DefaultTransport, journal: j}
	cfg.HTTPClient = &http.Client{Transport: tr, Timeout: 80 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client, err := openrouter.New(cfg)
	must(err)
	p := &subsetProvider{Provider: client, journal: j}
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 7, USD: .056})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = providers.WithBudget(ctx, ledger)
	began := time.Now()
	pdf, err := os.ReadFile(os.Getenv("POMNIBOT_EVAL_PDF"))
	must(err)
	if material.Digest(pdf) != "124e265eabf075590bbaef74b6cf98af64c99482f6340cc093686bcafc3b0198" {
		t.Fatal("PDF checksum differs")
	}
	ex, err := ingest.NewExtractor(nil, ingest.Options{AssessPDF: true})
	must(err)
	local, err := ex.Extract(ctx, []ingest.File{{Name: "SQL-original.pdf", Data: pdf}})
	must(err)
	must(j.save("local-reading.json", local))
	layers := make([]string, len(local.Document.Pages))
	for i, pg := range local.Document.Pages {
		layers[i] = pg.TextLayer
		if layers[i] == "" {
			layers[i] = pg.Reading
		}
	}
	d := material.Document{Version: material.Version, Digest: material.Digest(pdf)}
	readStarted := time.Now()
	reader := &ingest.VisionOCR{Model: p, FullPage: true, Concurrency: 1, CallTimeout: 80 * time.Second}
	var runErr error
	// Evaluation fixture selection only; no page number enters production logic.
	for _, n := range []int{28, 29} {
		got, e := reader.Recognize(ctx, ingest.OCRRequest{Data: pdf, MimeType: "application/pdf", Pages: 36, Only: []int{n - 1}, TextLayers: layers, Model: ingest.ModelScan})
		if e != nil {
			runErr = e
			break
		}
		pg := got[n-1]
		must(j.save(fmt.Sprintf("reading-page-%d.json", n), pg))
		if pg.ReadFailed || len(pg.Image) == 0 {
			runErr = errors.New("required visual reading failed; generation prohibited")
			break
		}
		d.Pages = append(d.Pages, material.Page{Number: n, OriginalPage: n, TextLayer: layers[n-1], Reading: pg.Text, Observations: pg.Observations, Issues: pg.Issues, Image: pg.Image, ImageMime: pg.ImageMime})
		blocks := pg.Blocks
		if len(blocks) == 0 {
			blocks = []material.Block{{Kind: "text", Text: pg.Text}}
		}
		for i, b := range blocks {
			b.ID = fmt.Sprintf("p%d-b%d", n, i+1)
			b.Page = n
			b.Order = i
			if len(b.Issues) == 0 {
				b.Issues = pg.Issues
			}
			d.Blocks = append(d.Blocks, b)
		}
		must(j.save(fmt.Sprintf("completed-reading-through-%d.json", n), d))
	}
	readingSeconds := time.Since(readStarted).Seconds()
	var first *float64
	var result generator.Result
	if runErr == nil {
		must(j.event("reading-complete", map[string]any{"seconds": readingSeconds, "pages": len(d.Pages)}))
		gen := generator.NewGenerator(p, generator.Options{Concurrency: 1, MaxCoverageRepairs: -1, CallTimeout: 80 * time.Second, OnBatch: func(b generator.Batch) {
			if first == nil {
				s := time.Since(began).Seconds()
				first = &s
			}
			if e := j.event("accepted-batch", b); e != nil {
				cancel()
				t.Errorf("persist accepted cards: %v", e)
			}
		}})
		result, runErr = gen.Generate(ctx, generator.Document{ID: "sql-subset-20261021", Title: "Введение в SQL (оригинальные страницы 28–29)", Structure: &d})
	}
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	result.Budget = ledger.Snapshot()
	must(j.save("result.json", map[string]any{"result": result, "readingSeconds": readingSeconds, "firstVerifiedSeconds": first, "elapsedSeconds": time.Since(began).Seconds(), "error": errText, "readingIdentity": ingest.ProcessingIdentity(), "readingVersion": material.Version, "pipelineVersion": generator.PipelineVersion}))
	must(j.event("run-complete", map[string]any{"error": errText, "cards": len(result.Cards), "budget": ledger.Snapshot()}))
	t.Logf("cards=%d attempts=%d committedUSD=%.7f error=%s", len(result.Cards), ledger.Snapshot().Calls, ledger.Snapshot().CommittedUSD, errText)
	if runErr != nil {
		t.Fatal(runErr)
	}
}

func TestSubsetJournalPreservesResponseOnProviderFailure(t *testing.T) {
	j := subsetJournal{t.TempDir()}
	p := &subsetProvider{Provider: subsetFake(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte("partial")}, context.DeadlineExceeded
	}), journal: j}
	_, err := p.Complete(context.Background(), providers.Request{System: "STAGE:REVIEW"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	for _, name := range []string{"request-01.json", "response-01.json", "events.jsonl"} {
		data, e := os.ReadFile(filepath.Join(j.dir, name))
		if e != nil || len(data) == 0 {
			t.Fatalf("missing durable %s: %v", name, e)
		}
	}
}
func TestSubsetGuardBlocksOversizedRequestBeforeProvider(t *testing.T) {
	calls := 0
	p := &subsetProvider{Provider: subsetFake(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{}, nil
	}), journal: subsetJournal{t.TempDir()}}
	_, err := p.Complete(context.Background(), providers.Request{User: strings.Repeat("x", 40000)})
	if !errors.Is(err, providers.ErrBudget) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

type subsetFake func(context.Context, providers.Request) (providers.Response, error)

func (f subsetFake) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return f(ctx, r)
}

// restoreSubsetLedger carries known and lost charges across a bounded continuation.
// It never represents restoration as a new transport request.
func restoreSubsetLedger(j subsetJournal) (*providers.Budget, error) {
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 7, USD: .056})
	for i := 1; i <= 3; i++ {
		var req struct {
			Bound float64 `json:"boundUSD"`
		}
		data, err := os.ReadFile(filepath.Join(j.dir, fmt.Sprintf("request-%02d.json", i)))
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		var response struct {
			Response providers.Response `json:"response"`
			Error    string             `json:"error"`
		}
		data, err = os.ReadFile(filepath.Join(j.dir, fmt.Sprintf("response-%02d.json", i)))
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &response); err != nil {
			return nil, err
		}
		finish, err := providers.ReserveCall(providers.WithBudget(context.Background(), ledger), req.Bound)
		if err != nil {
			return nil, err
		}
		finish(response.Response.Usage, response.Error == "")
	}
	return ledger, nil
}
func TestSubsetContinuationRetainsLostReserve(t *testing.T) {
	j := subsetJournal{t.TempDir()}
	for i := 1; i <= 3; i++ {
		if err := j.save(fmt.Sprintf("request-%02d.json", i), map[string]any{"boundUSD": .008}); err != nil {
			t.Fatal(err)
		}
		errText := ""
		if i == 3 {
			errText = "context deadline exceeded"
		}
		if err := j.save(fmt.Sprintf("response-%02d.json", i), map[string]any{"response": providers.Response{Usage: providers.Usage{CostUSD: .001}}, "error": errText}); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err := restoreSubsetLedger(j)
	if err != nil {
		t.Fatal(err)
	}
	if s := ledger.Snapshot(); s.Calls != 3 || s.CommittedUSD < .009999999 || s.ReportedUSD != .002 {
		t.Fatalf("lost charge discarded: %+v", s)
	}
	ctx := providers.WithBudget(context.Background(), ledger)
	for i := 0; i < 4; i++ {
		finish, e := providers.ReserveCall(ctx, .008)
		if e != nil {
			t.Fatal(e)
		}
		finish(providers.Usage{}, false)
	}
	if _, err = providers.ReserveCall(ctx, .008); !errors.Is(err, providers.ErrBudget) {
		t.Fatal("eighth attempt allowed")
	}
}

// Continues the SAME experiment once, carrying the original seven-attempt and
// $0.056 limits. No reading or successful response is purchased again.
func TestLiveSQLSubsetContinuation(t *testing.T) {
	if os.Getenv("POMNIBOT_LIVE_SQL_SUBSET_CONTINUE") != "1" {
		t.Skip("explicit opt-in required for bounded continuation")
	}
	j := subsetJournal{os.Getenv("POMNIBOT_SQL_SUBSET_DIR")}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(j.dir, "continuation-preflight.json"))
	must(err)
	var pre struct {
		Deadline time.Time `json:"deadlineUTC"`
	}
	must(json.Unmarshal(data, &pre))
	if time.Until(pre.Deadline) <= 0 {
		t.Fatal("original eight-minute wall deadline elapsed")
	}
	must(j.save("continuation-started.json", map[string]any{"atUTC": time.Now().UTC()}))
	data, err = os.ReadFile(filepath.Join(j.dir, "completed-reading-through-29.json"))
	must(err)
	var d material.Document
	must(json.Unmarshal(data, &d))
	if d.Version != material.Version || d.Digest != "124e265eabf075590bbaef74b6cf98af64c99482f6340cc093686bcafc3b0198" || len(d.Pages) != 2 {
		t.Fatal("reading identity mismatch")
	}
	for _, n := range []int{28, 29} {
		var pg ingest.OCRPage
		data, err = os.ReadFile(filepath.Join(j.dir, fmt.Sprintf("reading-page-%d.json", n)))
		must(err)
		must(json.Unmarshal(data, &pg))
		if pg.ReadFailed || len(pg.Image) == 0 {
			t.Fatal("unsuccessful reading")
		}
	}
	ledger, err := restoreSubsetLedger(j)
	must(err)
	cfg, err := openrouter.ConfigFromEnv()
	must(err)
	if cfg.Model != openrouter.DefaultModel || cfg.MaxTokens != 8000 || cfg.BaseURL != openrouter.DefaultBaseURL {
		t.Fatal("model changed")
	}
	tr := &subsetTransport{base: http.DefaultTransport, journal: j, attempts: 3}
	cfg.HTTPClient = &http.Client{Transport: tr, Timeout: 80 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client, err := openrouter.New(cfg)
	must(err)
	p := &subsetProvider{Provider: client, journal: j, next: 3}
	ctx, cancel := context.WithDeadline(context.Background(), pre.Deadline)
	defer cancel()
	ctx = providers.WithBudget(ctx, ledger)
	began := time.Now()
	var first *float64
	gen := generator.NewGenerator(p, generator.Options{Concurrency: 1, MaxCoverageRepairs: -1, CallTimeout: 80 * time.Second, OnBatch: func(b generator.Batch) {
		if first == nil {
			s := time.Since(began).Seconds()
			first = &s
		}
		if e := j.event("accepted-batch", b); e != nil {
			cancel()
			t.Errorf("persist: %v", e)
		}
	}})
	result, runErr := gen.Generate(ctx, generator.Document{ID: "sql-subset-20261021", Title: "Введение в SQL (оригинальные страницы 28–29)", Structure: &d})
	result.Budget = ledger.Snapshot()
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	must(j.save("continuation-result.json", map[string]any{"result": result, "firstVerifiedContinuationSeconds": first, "elapsedSeconds": time.Since(began).Seconds(), "error": errText}))
	must(j.event("continuation-complete", map[string]any{"budget": ledger.Snapshot(), "cards": len(result.Cards), "error": errText}))
	t.Logf("cards=%d totalAttempts=%d committedUSD=%.7f error=%s", len(result.Cards), ledger.Snapshot().Calls, ledger.Snapshot().CommittedUSD, errText)
	if runErr != nil {
		t.Fatal(runErr)
	}
}
