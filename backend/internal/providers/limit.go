package providers

import (
	"context"
	"errors"
	"fmt"
)

// ErrBusy is returned when a request gave up waiting for a free slot: the
// backend is busy with other requests of ours, not down.
var ErrBusy = errors.New("provider busy")

// limited is a Provider that lets at most cap(slots) requests run at once.
type limited struct {
	p     Provider
	slots chan struct{}
}

// Limit returns p letting at most n requests run at once; the rest wait their
// turn, as long as their context allows. It is for plans that answer requests
// over the limit with 429, such as GigaChat's one request at a time.
func Limit(p Provider, n int) Provider {
	return &limited{p: p, slots: make(chan struct{}, max(n, 1))}
}

func (l *limited) Complete(ctx context.Context, req Request) (Response, error) {
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return Response{}, fmt.Errorf("providers: waiting for a free slot: %w (%w)", ErrBusy, ctx.Err())
	}
	defer func() { <-l.slots }()
	return l.p.Complete(ctx, req)
}
