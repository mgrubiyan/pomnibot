package providers

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDocumentBudgetReservesConcurrentCallsAndUnknownCharges(t *testing.T) {
	b := NewBudget(BudgetLimits{Calls: 20, USD: .03, CallUSD: .01})
	ctx := WithBudget(context.Background(), b)
	var entered atomic.Int32
	var wg sync.WaitGroup
	hold := make(chan struct{})
	reserved := make(chan struct{}, 20)
	for range 20 {
		wg.Go(func() {
			finish, err := ReserveCall(ctx, .01)
			if errors.Is(err, ErrBudget) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			entered.Add(1)
			reserved <- struct{}{}
			<-hold
			finish(Usage{}, false) // a lost response may still have been billed
		})
	}
	// All reservations are done before releasing any: the monetary cap must hold.
	for range 3 {
		<-reserved
	}
	close(hold)
	wg.Wait()
	if entered.Load() != 3 || b.Snapshot().Calls != 3 || b.Snapshot().CommittedUSD > .03000001 {
		t.Fatalf("overspend: %+v entered=%d", b.Snapshot(), entered.Load())
	}
	if _, err := ReserveCall(ctx, .01); !errors.Is(err, ErrBudget) {
		t.Fatal("budget reopened after unknown charge", err)
	}
}

func TestDocumentBudgetSettlesKnownCostAndCountsEveryAttempt(t *testing.T) {
	b := NewBudget(BudgetLimits{Calls: 2, USD: .02, CallUSD: .01})
	ctx := WithBudget(context.Background(), b)
	finish, err := ReserveCall(ctx, .01)
	if err != nil {
		t.Fatal(err)
	}
	finish(Usage{CostUSD: .001}, true)
	finish, err = ReserveCall(ctx, .01)
	if err != nil {
		t.Fatal(err)
	}
	finish(Usage{CostUSD: .001}, true)
	if _, err = ReserveCall(ctx, .01); !errors.Is(err, ErrBudget) {
		t.Fatal("call limit ignored", err)
	}
	if b.Snapshot().CommittedUSD != .002 {
		t.Fatal(b.Snapshot())
	}
}

func TestInvalidBudgetNumbersCannotDisableCaps(t *testing.T) {
	for _, limits := range []BudgetLimits{{Calls: -1}, {USD: math.NaN()}, {USD: math.Inf(1)}, {USD: -1}, {CallUSD: math.NaN()}} {
		b := NewBudget(limits)
		if _, err := ReserveCall(WithBudget(context.Background(), b), .01); !errors.Is(err, ErrBudget) {
			t.Fatalf("invalid limits allowed spending: %+v", limits)
		}
	}
	b := NewBudget(BudgetLimits{Calls: 10, USD: .01})
	if _, err := ReserveCall(WithBudget(context.Background(), b), math.NaN()); !errors.Is(err, ErrBudget) {
		t.Fatal("invalid reservation allowed spending")
	}
}

func TestBudgetWaitsForUnusedConcurrentReservationInsteadOfDroppingWork(t *testing.T) {
	b := NewBudget(BudgetLimits{Calls: 2, USD: .01})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = WithBudget(ctx, b)
	first, err := ReserveCall(ctx, .01)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		finish, e := ReserveCall(ctx, .005)
		if e == nil {
			finish(Usage{CostUSD: .001}, true)
		}
		result <- e
	}()
	select {
	case e := <-result:
		first(Usage{}, true)
		t.Fatalf("temporary reservation pressure discarded work: %v", e)
	case <-time.After(20 * time.Millisecond):
	}
	first(Usage{CostUSD: .001}, true)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	if b.Snapshot().Calls != 2 || b.Snapshot().CommittedUSD != .002 {
		t.Fatal(b.Snapshot())
	}
}

func TestWaitingForBudgetCanBeCanceledWithoutMarkingProviderDown(t *testing.T) {
	b := NewBudget(BudgetLimits{Calls: 2, USD: .01})
	ctx := WithBudget(context.Background(), b)
	finish, err := ReserveCall(ctx, .01)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	_, err = ReserveCall(waitCtx, .005)
	finish(Usage{CostUSD: .001}, true)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrBusy) || b.Snapshot().Calls != 1 {
		t.Fatalf("budget queue cancellation misclassified: %v %+v", err, b.Snapshot())
	}
}
