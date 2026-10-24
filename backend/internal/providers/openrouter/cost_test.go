package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func estimateFor(t *testing.T, c *Client, req providers.Request) costEstimate {
	t.Helper()
	body, err := c.requestBody(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return c.estimateCost(req, body)
}

func TestInputReservationStaysNearRecordedUsage(t *testing.T) {
	// Representative input sizes from the October 2026 artifacts. Repeated runs
	// are omitted; the native PDF example represents one 27-page document.
	for _, tc := range []struct {
		name                                           string
		textBytes, pdfTextBytes, pages, reportedTokens int
	}{
		{"short text", 13005, 0, 0, 4376},
		{"lecture text", 77215, 0, 0, 21148},
		{"extracted reading", 71975, 0, 0, 24494},
		{"native PDF", 20377, 26105, 27, 54317},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := providers.Request{}
			if tc.pages > 0 {
				req.PDFs = []providers.PDF{{Pages: tc.pages, TextBytes: tc.pdfTextBytes}}
			}
			body := chatRequest{Model: DefaultModel, MaxTokens: 128000, Messages: []message{{Content: strings.Repeat("x", tc.textBytes)}}}
			e := (&Client{}).estimateCost(req, body)
			// Cover observed usage while keeping the input overhead below 75%.
			// A conservative byte-per-token or 16K-per-page regression must fail too.
			if e.ReservedInputTokens < tc.reportedTokens || float64(e.ReservedInputTokens) > 1.75*float64(tc.reportedTokens) {
				t.Errorf("input reservation %d outside [%d, %.0f] tokens", e.ReservedInputTokens, tc.reportedTokens, 1.75*float64(tc.reportedTokens))
			}
		})
	}
}

func TestTextPDFLecturesFitDocumentBudget(t *testing.T) {
	for _, tc := range []struct{ pages, textBytes int }{{45, 24519}, {53, 35498}} {
		t.Run(fmt.Sprint(tc.pages), func(t *testing.T) {
			budget := providers.NewBudget(providers.BudgetLimits{USD: .15, Calls: 3})
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body chatRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.MaxTokens != 128000 || body.Reasoning.Effort != "medium" {
					t.Errorf("generation settings changed: max_tokens=%d effort=%s", body.MaxTokens, body.Reasoning.Effort)
				}
				if reserved := budget.Snapshot().ReservedUSD; reserved < .064 || reserved > .09 {
					t.Errorf("unexpected reservation: %g", reserved)
				}
				_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.02}}`)
			})
			req := providers.Request{System: strings.Repeat("x", 18000), Source: "source", User: "generate cards", MaxTokens: 128000, ReasoningEffort: "medium",
				PDFs: []providers.PDF{{Name: "lecture.pdf", Data: []byte("%PDF"), Pages: tc.pages, TextBytes: tc.textBytes}}}
			_, err := c.Complete(providers.WithBudget(context.Background(), budget), req)
			if err != nil || calls != 1 || budget.Snapshot().CommittedUSD != .02 || budget.Snapshot().ReservedUSD != 0 {
				t.Fatalf("lecture rejected or charged incorrectly: calls=%d budget=%+v err=%v", calls, budget.Snapshot(), err)
			}
		})
	}
}

func TestReservationUsesSentOutputLimit(t *testing.T) {
	for _, tc := range []struct {
		name              string
		override, wantCap int
		budgetUSD         float64
	}{
		{"lower", 256, 256, .001},
		{"higher", 128000, 128000, .07},
		{"inherited", 0, 50000, .03},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body chatRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.MaxTokens != tc.wantCap {
					t.Errorf("sent %d output tokens, want %d", body.MaxTokens, tc.wantCap)
				}
				_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.00001}}`)
			})
			c.cfg.MaxTokens = 50000
			budget := providers.NewBudget(providers.BudgetLimits{USD: tc.budgetUSD})
			_, err := c.Complete(providers.WithBudget(context.Background(), budget), providers.Request{User: "hello", MaxTokens: tc.override})
			if err != nil || calls != 1 {
				t.Fatalf("output reservation disagrees with request: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestPreparedTextAndCacheAreCounted(t *testing.T) {
	c := clientFor(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected network request") })
	req := providers.Request{System: "правила", User: "задача", Source: "источник", Schema: json.RawMessage(`{"type":"object"}`),
		PDFs: []providers.PDF{{Name: "notes.pdf", Pages: 2, TextBytes: 123}, {Name: "extra.pdf", Pages: 3, TextBytes: 456}}}
	e := estimateFor(t, c, req)
	// Includes the generated source instructions, both file captions and the separate task message.
	if e.TextBytes != 202 || e.PDFPages != 5 || e.PDFTextBytes != 579 {
		t.Fatalf("prepared input not fully counted: %+v", e)
	}
	withoutCache, err := c.requestBody(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	parts := withoutCache.Messages[1].Content.([]part)
	for i := range parts {
		parts[i].CacheControl = nil
	}
	plain := c.estimateCost(req, withoutCache)
	outputUSD := float64(e.MaxOutputTokens) * maxCompletionPrice / 1e6
	if e.ReservedInputTokens != plain.ReservedInputTokens || math.Abs((e.ReservedUSD-outputUSD)/(plain.ReservedUSD-outputUSD)-1.25) > 1e-9 {
		t.Fatalf("cache write markup must be separate from token uncertainty: cached=%+v uncached=%+v", e, plain)
	}
	// Binary encoding size is not text. Replacing the PDF bytes must not change the estimate.
	req.PDFs[0].Data = bytes.Repeat([]byte("pdf"), 10000)
	if larger := estimateFor(t, c, req); larger != e {
		t.Fatalf("encoded PDF counted as text: before=%+v after=%+v", e, larger)
	}
}

func TestUncalibratedInputsKeepConservativeReservation(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		req         providers.Request
	}{
		{"other model", "other/model", providers.Request{User: "text"}},
		{"rolling alias", "~anthropic/claude-haiku-latest", providers.Request{User: "text"}},
		{"image", DefaultModel, providers.Request{Image: &providers.Image{Data: []byte("unknown format")}}},
		{"scan", DefaultModel, providers.Request{PDFs: []providers.PDF{{Pages: 4}}}},
		{"mixed PDFs", DefaultModel, providers.Request{PDFs: []providers.PDF{{Pages: 2, TextBytes: 500}, {Pages: 3}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected network request") })
			c.cfg.Model = tc.model
			e := estimateFor(t, c, tc.req)
			if e.Version != conservativeCostVersion || e.ReservedInputTokens != c.inputBound(tc.req) {
				t.Fatalf("uncalibrated request used empirical estimate: %+v", e)
			}
		})
	}
	c := clientFor(t, func(http.ResponseWriter, *http.Request) {})
	c.cfg.Model += ":nitro"
	if e := estimateFor(t, c, providers.Request{User: "text"}); e.Version != empiricalCostVersion {
		t.Fatalf("model variant lost calibrated estimate: %+v", e)
	}
}

func TestEmpiricalReservationStillRejectsInsufficientBudget(t *testing.T) {
	c := clientFor(t, func(http.ResponseWriter, *http.Request) { t.Error("budget rejection reached transport") })
	budget := providers.NewBudget(providers.BudgetLimits{USD: .06})
	_, err := c.Complete(providers.WithBudget(context.Background(), budget), providers.Request{MaxTokens: 128000})
	if !errors.Is(err, providers.ErrBudget) || budget.Snapshot().Calls != 0 {
		t.Fatalf("output ceiling escaped budget: %+v %v", budget.Snapshot(), err)
	}
}

func TestActualCostAboveEstimateStillSettlesAndStopsBudget(t *testing.T) {
	calls := 0
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.01}}`)
	})
	budget := providers.NewBudget(providers.BudgetLimits{USD: .15})
	ctx := providers.WithBudget(context.Background(), budget)
	if _, err := c.Complete(ctx, providers.Request{User: "hello"}); err != nil {
		t.Fatal(err)
	}
	if s := budget.Snapshot(); !s.Stopped || s.CommittedUSD != .01 || s.ReportedUSD != .01 || s.ReservedUSD != 0 {
		t.Fatalf("underestimated actual cost was not settled: %+v", s)
	}
	if _, err := c.Complete(ctx, providers.Request{User: "hello"}); !errors.Is(err, providers.ErrBudget) || calls != 1 {
		t.Fatalf("underestimated reservation allowed further spending: calls=%d err=%v", calls, err)
	}
}

func TestCostEstimateLogsDoNotLeakContent(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"private-answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100000,"cost":0.00001}}`)
	})
	req := providers.Request{System: "private-system", User: "private-user", PDFs: []providers.PDF{{Name: "private-filename", Pages: 1, TextBytes: 20, Data: []byte("private-file")}}}
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	for _, want := range []string{empiricalCostVersion, "estimated_input_tokens", "reserved_input_tokens", "reserved_cost_usd", "input token reservation underestimated"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing estimate diagnostic %q", want)
		}
	}
	for _, secret := range []string{"private-", "secret", "cHJpdmF0ZS1maWxl"} {
		if strings.Contains(text, secret) {
			t.Errorf("cost diagnostics leaked request or response data")
		}
	}
}
