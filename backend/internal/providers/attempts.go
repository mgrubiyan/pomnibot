package providers

import (
	"context"
	"sync"
)

type attemptLimitKey struct{}
type attemptLimit struct {
	mu        sync.Mutex
	remaining int
	parent    *attemptLimit
}

// WithAttemptLimit adds a transport cap without replacing the monetary ledger
// or relaxing a stricter enclosing cap. ReserveCall enforces all enclosing caps.
func WithAttemptLimit(ctx context.Context, n int) context.Context {
	parent, _ := ctx.Value(attemptLimitKey{}).(*attemptLimit)
	return context.WithValue(ctx, attemptLimitKey{}, &attemptLimit{remaining: n, parent: parent})
}

func (l *attemptLimit) take() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.remaining <= 0 {
		return ErrBudget
	}
	if err := l.parent.take(); err != nil {
		return err
	}
	l.remaining--
	return nil
}
