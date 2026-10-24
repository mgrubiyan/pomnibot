package providers

import (
	"context"
	"errors"
	"math"
	"sync"
)

// ErrBudget stops new paid attempts without invalidating already validated cards.
var ErrBudget = errors.New("document provider budget exhausted")

// BudgetLimits are shared by reading, generation, retries and fallback requests.
// CallUSD is a conservative charge reservation for backends without price metadata.
type BudgetLimits struct {
	Calls        int
	USD, CallUSD float64
}

// BudgetSnapshot distinguishes reported charges from reserved/unknown charges.
type BudgetSnapshot struct {
	Calls        int     `json:"calls"`
	ReportedUSD  float64 `json:"reportedUsd"`
	CommittedUSD float64 `json:"committedUsd"`
	ReservedUSD  float64 `json:"reservedUsd"`
	Stopped      bool    `json:"stopped"`
}

// Budget atomically reserves future cost, including concurrent in-flight attempts.
type Budget struct {
	mu      sync.Mutex
	limits  BudgetLimits
	state   BudgetSnapshot
	changed chan struct{}
}
type budgetKey struct{}

// DefaultDocumentLimits bounds ordinary processing without coupling document and request concurrency.
func DefaultDocumentLimits() BudgetLimits { return BudgetLimits{Calls: 96} }

// NewBudget creates an independent document ledger. Zero limits disable that cap.
func NewBudget(l BudgetLimits) *Budget {
	b := &Budget{limits: l, changed: make(chan struct{})}
	b.state.Stopped = l.Calls < 0 || invalidAmount(l.USD) || invalidAmount(l.CallUSD)
	return b
}

func invalidAmount(n float64) bool { return n < 0 || math.IsNaN(n) || math.IsInf(n, 0) }

// WithBudget propagates one ledger through every processing stage.
func WithBudget(ctx context.Context, b *Budget) context.Context {
	return context.WithValue(ctx, budgetKey{}, b)
}

// BudgetFrom returns the document ledger, if any.
func BudgetFrom(ctx context.Context) *Budget { b, _ := ctx.Value(budgetKey{}).(*Budget); return b }

// MonetaryLimited reports whether price bounds are required for this ledger.
func (b *Budget) MonetaryLimited() bool { return b.limits.USD > 0 }

// Snapshot returns a consistent copy of all reservations and charges.
func (b *Budget) Snapshot() BudgetSnapshot { b.mu.Lock(); defer b.mu.Unlock(); return b.state }

// ReserveCall must run at the transport attempt, inside any retries/fallback.
// Unknown/lost responses retain their full reservation; they are never free.
// The reservation must use the provider's configured prices and output cap.
// Input token estimates may be empirical; this ledger cannot guarantee a spend cap.
func ReserveCall(ctx context.Context, bound float64) (func(Usage, bool), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if invalidAmount(bound) {
		return nil, ErrBudget
	}
	b := BudgetFrom(ctx)
	if b == nil {
		return func(Usage, bool) {}, nil
	}
	b.mu.Lock()
	if bound <= 0 {
		bound = b.limits.CallUSD
	}
	for {
		if err := ctx.Err(); err != nil {
			b.mu.Unlock()
			return nil, err
		}
		if b.state.Stopped || b.limits.Calls > 0 && b.state.Calls >= b.limits.Calls || b.limits.USD > 0 && (bound <= 0 || b.state.CommittedUSD+bound > b.limits.USD+1e-12) {
			b.mu.Unlock()
			return nil, ErrBudget
		}
		if b.limits.USD <= 0 || b.state.CommittedUSD+b.state.ReservedUSD+bound <= b.limits.USD+1e-12 {
			break
		}
		// In-flight work can release unused reservation. Wait rather than
		// incorrectly declaring the entire document's budget exhausted.
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, errors.Join(ErrBusy, ctx.Err())
		case <-changed:
		}
		b.mu.Lock()
	}
	b.state.Calls++
	b.state.ReservedUSD += bound
	b.mu.Unlock()
	var once sync.Once
	return func(u Usage, known bool) {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.state.ReservedUSD -= bound
			charged := bound
			if known && !invalidAmount(u.CostUSD) {
				charged = u.CostUSD
				b.state.ReportedUSD += charged
			}
			b.state.CommittedUSD += charged
			if b.limits.USD > 0 && (charged > bound+1e-12 || b.state.CommittedUSD >= b.limits.USD) {
				b.state.Stopped = true
			}
			if b.changed != nil {
				close(b.changed)
			}
			b.changed = make(chan struct{})
		})
	}, nil
}
