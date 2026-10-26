package providers

import (
	"context"
	"errors"
	"testing"
)

func TestAttemptLimitsPreserveEnclosingCapsAndMoney(t *testing.T) {
	ledger := NewBudget(BudgetLimits{Calls: 10, USD: .1})
	root := WithBudget(context.Background(), ledger)
	finish, err := ReserveCall(root, .03)
	if err != nil {
		t.Fatal(err)
	}
	finish(Usage{CostUSD: .02}, true)
	root = WithAttemptLimit(root, 2)
	for i := 0; i < 2; i++ {
		stage := WithAttemptLimit(root, 1)
		finish, err = ReserveCall(stage, .01)
		if err != nil {
			t.Fatal(err)
		}
		finish(Usage{CostUSD: .005}, true)
		if _, err = ReserveCall(stage, .01); !errors.Is(err, ErrBudget) {
			t.Fatal("stage cap escaped", err)
		}
	}
	if _, err = ReserveCall(root, .01); !errors.Is(err, ErrBudget) {
		t.Fatal("flow cap escaped", err)
	}
	if s := ledger.Snapshot(); s.Calls != 3 || s.CommittedUSD < .029999 || s.CommittedUSD > .030001 {
		t.Fatalf("reset original ledger: %+v", s)
	}
	stricter := WithAttemptLimit(WithAttemptLimit(context.Background(), 1), 2)
	finish, err = ReserveCall(stricter, 0)
	if err != nil {
		t.Fatal(err)
	}
	finish(Usage{}, true)
	if _, err = ReserveCall(stricter, 0); !errors.Is(err, ErrBudget) {
		t.Fatal("relaxed caller cap", err)
	}
}
