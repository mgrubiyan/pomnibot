package generator

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type stageTimeoutProvider func(context.Context, providers.Request) (providers.Response, error)

func (f stageTimeoutProvider) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return f(ctx, r)
}

func TestStageRecoversFromOneCallTimeoutWithFreshContext(t *testing.T) {
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 2, USD: .01})
	ctx, cancel := context.WithTimeout(providers.WithBudget(context.Background(), ledger), time.Second)
	defer cancel()
	calls := 0
	model := stageTimeoutProvider(func(callCtx context.Context, _ providers.Request) (providers.Response, error) {
		if callCtx.Err() != nil {
			t.Fatal("retry inherited expired call context")
		}
		finish, err := providers.ReserveCall(callCtx, .003)
		if err != nil {
			return providers.Response{}, err
		}
		calls++
		if calls == 1 {
			<-callCtx.Done()
			finish(providers.Usage{}, false)
			return providers.Response{}, callCtx.Err()
		}
		u := providers.Usage{CostUSD: .001}
		finish(u, true)
		return providers.Response{Model: "fake", Content: []byte(`{"topics":[]}`), Usage: u}, nil
	})
	g := NewGenerator(model, Options{CallTimeout: 5 * time.Millisecond})
	var out Plan
	var stats Stats
	if err := g.stage(ctx, "STAGE:PLAN", []string{"source"}, json.RawMessage(`{"type":"object"}`), &out, &stats); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || stats.ModelCalls != 2 || out.Topics == nil {
		t.Fatalf("no recovered response: calls=%d stats=%+v out=%+v", calls, stats, out)
	}
	if s := ledger.Snapshot(); s.Calls != 2 || math.Abs(s.CommittedUSD-.004) > 1e-12 || s.ReportedUSD != .001 {
		t.Fatalf("lost charge or retry escaped ledger: %+v", s)
	}
}

func TestStageTimeoutRetryRespectsParentAndBudget(t *testing.T) {
	for _, mode := range []string{"parent cancellation", "parent deadline", "attempt limit", "money limit", "repeated timeout", "refusal"} {
		t.Run(mode, func(t *testing.T) {
			limits := providers.BudgetLimits{Calls: 2, USD: .01}
			if mode == "attempt limit" {
				limits.Calls = 1
			}
			if mode == "money limit" {
				limits.USD = .004
			}
			ledger := providers.NewBudget(limits)
			parent, cancel := context.WithCancel(providers.WithBudget(context.Background(), ledger))
			defer cancel()
			if mode == "parent deadline" {
				var done context.CancelFunc
				parent, done = context.WithTimeout(parent, 5*time.Millisecond)
				defer done()
			}
			calls := 0
			model := stageTimeoutProvider(func(callCtx context.Context, _ providers.Request) (providers.Response, error) {
				finish, err := providers.ReserveCall(callCtx, .003)
				if err != nil {
					return providers.Response{}, err
				}
				calls++
				if mode == "refusal" {
					finish(providers.Usage{}, false)
					return providers.Response{}, providers.ErrRefused
				}
				if mode == "parent cancellation" {
					cancel()
				}
				<-callCtx.Done()
				finish(providers.Usage{}, false)
				return providers.Response{}, callCtx.Err()
			})
			timeout := 5 * time.Millisecond
			if mode == "parent deadline" {
				timeout = time.Second
			}
			g := NewGenerator(model, Options{CallTimeout: timeout})
			var out Plan
			err := g.stage(parent, "STAGE:PLAN", nil, nil, &out, nil)
			want := context.DeadlineExceeded
			wantCalls := 1
			switch mode {
			case "parent cancellation":
				want = context.Canceled
			case "attempt limit", "money limit":
				want = providers.ErrBudget
			case "repeated timeout":
				wantCalls = 2
			case "refusal":
				want = providers.ErrRefused
			}
			if !errors.Is(err, want) || calls != wantCalls {
				t.Fatalf("err=%v want=%v calls=%d want=%d", err, want, calls, wantCalls)
			}
		})
	}
}
