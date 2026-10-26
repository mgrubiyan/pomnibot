package providers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A plan that serves one request at a time answers a second one with 429:
// Limit makes the extra requests wait their turn instead.
func TestLimitKeepsRequestsWithinTheLimit(t *testing.T) {
	var running, peak atomic.Int32
	p := &fakeProvider{reply: func(context.Context, int) (Response, error) {
		n := running.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		running.Add(-1)
		return answer("GigaChat-3-Ultra")
	}}
	limited := Limit(p, 1)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := limited.Complete(context.Background(), testRequest()); err != nil {
				t.Errorf("Complete() error = %v", err)
			}
		})
	}
	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Errorf("%d requests at once, want 1", got)
	}
	if got := p.callCount(); got != 8 {
		t.Errorf("%d calls, want 8", got)
	}
}

func TestLimitStopsWaitingWhenCallerGivesUp(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	p := &fakeProvider{reply: func(context.Context, int) (Response, error) {
		close(started)
		<-release
		return answer("GigaChat-3-Ultra")
	}}
	limited := Limit(p, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = limited.Complete(context.Background(), testRequest())
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := limited.Complete(ctx, testRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Complete() error = %v, want context.DeadlineExceeded while waiting", err)
	}
	if got := p.callCount(); got != 1 {
		t.Errorf("%d calls, want 1: the waiting request never ran", got)
	}
	close(release)
	<-done
}
