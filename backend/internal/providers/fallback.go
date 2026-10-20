package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrUnavailable is returned when the backend cannot serve anyone for a
// while: it is down or keeps failing after retries, out of quota, or rejects
// the credentials. Fallback skips such a backend for its cooldown.
var ErrUnavailable = errors.New("provider unavailable")

// Backend is one provider in a Fallback chain.
type Backend struct {
	Name     string // for logs: gigachat, yandexgpt
	Provider Provider
	// Timeout bounds one call to this backend, its retries included, so that
	// a backend that hangs leaves time for the next one. When it fires, the
	// backend counts as unavailable. 0: only the caller's deadline.
	Timeout time.Duration
}

// Fallback is a Provider over several backends, tried in order: a request
// goes to the next one when a backend fails, and a backend that is
// unavailable is skipped for the cooldown. It is safe for concurrent use.
type Fallback struct {
	backends []Backend
	cooldown time.Duration
	now      func() time.Time

	mu        sync.Mutex
	downUntil map[int]time.Time // backend index → skipped until
}

var _ Provider = (*Fallback)(nil)

// NewFallback returns a Fallback over backends, the first one preferred.
func NewFallback(cooldown time.Duration, backends ...Backend) *Fallback {
	return &Fallback{
		backends:  backends,
		cooldown:  cooldown,
		now:       time.Now,
		downUntil: map[int]time.Time{},
	}
}

// Complete sends the request to the first backend in service that answers.
// When every backend fails, the error joins theirs, so errors.Is sees
// ErrRefused or ErrUnavailable from any of them.
func (f *Fallback) Complete(ctx context.Context, req Request) (Response, error) {
	order := f.inService()
	var errs []error
	for i, idx := range order {
		b := f.backends[idx]
		resp, err := f.call(ctx, b, req)
		if err == nil {
			f.markUp(idx)
			return resp, nil
		}
		if ctx.Err() != nil {
			// The caller gave up: no time for another backend, and this one
			// is not to blame.
			return Response{}, fmt.Errorf("providers: %w (last error: %w)", ctx.Err(), err)
		}
		if errors.Is(err, ErrUnavailable) {
			f.markDown(idx, b.Name, err)
		}
		errs = append(errs, err)
		if i+1 < len(order) {
			slog.Warn("providers: switching to the next backend",
				"backend", b.Name, "next", f.backends[order[i+1]].Name, "err", err)
		}
	}
	return Response{}, errors.Join(errs...)
}

// call runs one backend under its own timeout. A timeout that fires before
// the caller's deadline means the backend hangs: it counts as unavailable,
// unless the request was still waiting for a slot (ErrBusy).
func (f *Fallback) call(ctx context.Context, b Backend, req Request) (Response, error) {
	if b.Timeout <= 0 {
		return b.Provider.Complete(ctx, req)
	}
	callCtx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	resp, err := b.Provider.Complete(callCtx, req)
	if err != nil && ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) && !errors.Is(err, ErrBusy) {
		err = fmt.Errorf("%s: no answer in %s: %w (%w)", b.Name, b.Timeout, ErrUnavailable, err)
	}
	return resp, err
}

// inService lists the backends to try in order, skipping those cooling down.
// When all are, all are tried: one may be back, and failing outright is no
// better.
func (f *Fallback) inService() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	order := make([]int, 0, len(f.backends))
	for i := range f.backends {
		if until, ok := f.downUntil[i]; ok && now.Before(until) {
			continue
		}
		order = append(order, i)
	}
	if len(order) == 0 {
		for i := range f.backends {
			order = append(order, i)
		}
	}
	return order
}

func (f *Fallback) markDown(idx int, name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downUntil[idx] = f.now().Add(f.cooldown)
	slog.Warn("providers: backend unavailable, skipping it", "backend", name, "cooldown", f.cooldown, "err", err)
}

func (f *Fallback) markUp(idx int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.downUntil, idx)
}
