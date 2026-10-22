package openrouter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestCompleteStreamsContentAndUsageWithoutWaitingForEOF(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) || !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Error("production client did not request streaming")
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\r\n\r\n"+
			"event: message\r\ndata: {\"id\":\"gen-test\",\"model\":\"actual/model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"При\"}}]}\r\n\r\n"+
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"вет\"},\"finish_reason\":\"length\"}]}\n\n"+
			"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\n"+
			"data: \"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"cost\":0.001}}\n\n"+
			"data: [DONE]\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	budget := providers.NewBudget(providers.BudgetLimits{Calls: 1, USD: .02})
	res, err := c.Complete(providers.WithBudget(ctx, budget), providers.Request{User: "private document"})
	if err != nil || string(res.Content) != "Привет" || res.Model != "actual/model" || !res.Truncated || res.Usage.CompletionTokens != 20 {
		t.Fatalf("response=%+v error=%v", res, err)
	}
	if s := budget.Snapshot(); s.ReportedUSD != .001 || s.CommittedUSD != .001 || s.ReservedUSD != 0 {
		t.Fatalf("stream usage not accounted: %+v", s)
	}
}

func TestIncompleteStreamsNeverReturnPartialCards(t *testing.T) {
	for _, tc := range []struct{ name, stream string }{
		{"missing DONE", "data: {\"choices\":[{\"delta\":{\"content\":\"private partial\"},\"finish_reason\":\"stop\"}]}\n\n"},
		{"DONE before finish", "data: {\"choices\":[{\"delta\":{\"content\":\"private partial\"}}]}\n\ndata: [DONE]\n\n"},
		{"invalid event", "data: not-json-private\n\ndata: [DONE]\n\n"},
		{"conflicting finish", "data: {\"choices\":[{\"delta\":{\"content\":\"private partial\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.stream)
			})
			budget := providers.NewBudget(providers.BudgetLimits{Calls: 1, USD: .02})
			res, err := c.Complete(providers.WithBudget(context.Background(), budget), providers.Request{})
			if err == nil || len(res.Content) != 0 || strings.Contains(err.Error(), "private") {
				t.Fatalf("partial response accepted or leaked: %+v %v", res, err)
			}
			if budget.Snapshot().CommittedUSD <= 0 {
				t.Fatal("lost stream treated as free")
			}
		})
	}
}

func TestStreamingProviderErrorRetriesAndDoesNotLeakContent(t *testing.T) {
	calls := 0
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"private partial\"}}]}\n\n"+
			"data: {\"error\":{\"code\":\"server_error\",\"message\":\"private provider error\"},\"choices\":[{\"delta\":{},\"finish_reason\":\"error\"}],\"usage\":{\"cost\":0.001}}\n\n")
	})
	budget := providers.NewBudget(providers.BudgetLimits{Calls: 3, USD: .02})
	res, err := c.Complete(providers.WithBudget(context.Background(), budget), providers.Request{})
	if err == nil || !errors.Is(err, providers.ErrUnavailable) || calls != 3 || len(res.Content) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("response=%+v calls=%d error=%v", res, calls, err)
	}
	if s := budget.Snapshot(); s.ReportedUSD != .003 || s.CommittedUSD != .003 {
		t.Fatalf("error usage lost: %+v", s)
	}
}

func TestStreamingRefusal(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"refusal\":\"no\"},\"finish_reason\":\"content_filter\"}]}\n\ndata: [DONE]\n\n")
	})
	_, err := c.Complete(context.Background(), providers.Request{})
	if !errors.Is(err, providers.ErrRefused) {
		t.Fatal(err)
	}
}

func TestStreamingUsageWithoutCostRetainsTokensAndReservation(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n")
	})
	b := providers.NewBudget(providers.BudgetLimits{Calls: 1, USD: .02})
	res, err := c.Complete(providers.WithBudget(context.Background(), b), providers.Request{})
	if err != nil || res.Usage.PromptTokens != 10 || res.Usage.CompletionTokens != 20 {
		t.Fatalf("usage lost: %+v %v", res, err)
	}
	if s := b.Snapshot(); s.ReportedUSD != 0 || s.CommittedUSD <= 0 {
		t.Fatalf("unreported charge treated as free: %+v", s)
	}
}

func TestStreamSizeLimit(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ":"+strings.Repeat("x", maxBodyBytes)+"\n\n")
	})
	_, err := c.Complete(context.Background(), providers.Request{})
	if !errors.Is(err, errResponseSize) {
		t.Fatal(err)
	}
}
