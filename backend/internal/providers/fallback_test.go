package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// TestMain silences the fallback's warnings: tests switch backends on purpose.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

const testCooldown = 10 * time.Minute

func TestFallbackReservesDeadlineForEveryBackup(t *testing.T) {
	hang := func(ctx context.Context, _ int) (Response, error) { <-ctx.Done(); return Response{}, ctx.Err() }
	first, second, last := &fakeProvider{reply: hang}, &fakeProvider{reply: hang}, answers("last")
	f := NewFallback(time.Minute, Backend{Name: "first", Provider: first, Timeout: time.Second}, Backend{Name: "second", Provider: second, Timeout: time.Second}, Backend{Name: "last", Provider: last})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := f.Complete(ctx, Request{})
	if err != nil || res.Model != "last" || last.callCount() != 1 {
		t.Fatalf("final backend got no time: %+v %v", res, err)
	}
}

func TestFallbackShortCallerBudgetDoesNotCoolProvider(t *testing.T) {
	primary := &fakeProvider{reply: func(ctx context.Context, call int) (Response, error) {
		if call == 1 {
			<-ctx.Done()
			return Response{}, ctx.Err()
		}
		return Response{Model: "primary"}, nil
	}}
	f := NewFallback(time.Minute, Backend{Name: "primary", Provider: primary, Timeout: time.Second}, Backend{Name: "backup", Provider: answers("backup")})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.Complete(ctx, Request{}); err != nil {
		t.Fatal(err)
	}
	res, err := f.Complete(context.Background(), Request{})
	if err != nil || res.Model != "primary" || primary.callCount() != 2 {
		t.Fatalf("short caller budget cooled the primary: %+v %v", res, err)
	}
}

// fakeProvider answers with reply, given the number of the call, from 1.
type fakeProvider struct {
	reply func(ctx context.Context, call int) (Response, error)

	mu    sync.Mutex
	calls int
	reqs  []Request
}

func (p *fakeProvider) Complete(ctx context.Context, req Request) (Response, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	return p.reply(ctx, n)
}

func (p *fakeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func answer(model string) (Response, error) {
	return Response{Content: []byte(`{"facts":[]}`), Model: model}, nil
}

func answers(model string) *fakeProvider {
	return &fakeProvider{reply: func(context.Context, int) (Response, error) { return answer(model) }}
}

func fails(err error) *fakeProvider {
	return &fakeProvider{reply: func(context.Context, int) (Response, error) { return Response{}, err }}
}

// failsFirst fails the first call with err and answers after.
func failsFirst(err error, model string) *fakeProvider {
	return &fakeProvider{reply: func(_ context.Context, call int) (Response, error) {
		if call == 1 {
			return Response{}, err
		}
		return answer(model)
	}}
}

// Errors as the real providers wrap them.
var (
	errDown    = fmt.Errorf("gigachat: chat GigaChat-3-Ultra: status 503: %w", ErrUnavailable)
	errRefused = fmt.Errorf("gigachat: GigaChat-3-Ultra: %w", ErrRefused)
)

// clock is a manual time source for cooldowns.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestFallback(backends ...Backend) (*Fallback, *clock) {
	f := NewFallback(testCooldown, backends...)
	c := &clock{t: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	f.now = c.Now
	return f, c
}

func testRequest() Request {
	return Request{
		System:      "Составь карточки по конспекту.",
		User:        "Митоз — непрямое деление клеток.",
		Schema:      []byte(`{"type":"object"}`),
		Temperature: 0.3,
		Image:       &Image{Data: []byte{0xff, 0xd8}, MimeType: "image/jpeg"},
	}
}

func TestFallbackUsesPrimaryWhenItAnswers(t *testing.T) {
	primary, secondary := answers("GigaChat-3-Ultra"), answers("yandexgpt")
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})

	resp, err := f.Complete(context.Background(), testRequest())
	if err != nil || resp.Model != "GigaChat-3-Ultra" {
		t.Fatalf("got %+v, %v; want the primary's answer", resp, err)
	}
	if secondary.callCount() != 0 {
		t.Errorf("secondary called %d times, want 0", secondary.callCount())
	}
}

func TestFallbackSwitchesWhenPrimaryUnavailable(t *testing.T) {
	primary, secondary := fails(errDown), answers("yandexgpt")
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})

	resp, err := f.Complete(context.Background(), testRequest())
	if err != nil || resp.Model != "yandexgpt" {
		t.Fatalf("got %+v, %v; want the secondary's answer", resp, err)
	}
	if len(secondary.reqs) != 1 || !reflect.DeepEqual(secondary.reqs[0], testRequest()) {
		t.Errorf("secondary got %+v, want the request unchanged", secondary.reqs)
	}
}

// An unavailable backend is skipped for the cooldown, so requests do not
// wait out its retries one after another, and tried again after it.
func TestFallbackSkipsUnavailablePrimaryUntilCooldownEnds(t *testing.T) {
	primary, secondary := failsFirst(errDown, "GigaChat-3-Ultra"), answers("yandexgpt")
	f, clk := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})
	ctx := context.Background()

	if _, err := f.Complete(ctx, testRequest()); err != nil {
		t.Fatalf("first call: %v", err)
	}

	clk.Advance(testCooldown - time.Second)
	resp, err := f.Complete(ctx, testRequest())
	if err != nil || resp.Model != "yandexgpt" {
		t.Fatalf("during cooldown: got %+v, %v; want the secondary's answer", resp, err)
	}
	if primary.callCount() != 1 {
		t.Errorf("during cooldown: primary called %d times, want 1: skipped", primary.callCount())
	}

	clk.Advance(2 * time.Second)
	resp, err = f.Complete(ctx, testRequest())
	if err != nil || resp.Model != "GigaChat-3-Ultra" {
		t.Fatalf("after cooldown: got %+v, %v; want the primary back", resp, err)
	}
	if secondary.callCount() != 2 {
		t.Errorf("after cooldown: secondary called %d times, want 2", secondary.callCount())
	}
}

// Other failures are about this request, not the backend: the next backend
// may take it, but the failed one stays first in line.
func TestFallbackTriesNextWithoutCooldownOnRequestErrors(t *testing.T) {
	for _, reqErr := range []error{
		errRefused, // another provider has its own content filter
		errors.New("gigachat: chat GigaChat-3-Ultra: status 400: bad request"),
	} {
		t.Run(reqErr.Error(), func(t *testing.T) {
			primary, secondary := failsFirst(reqErr, "GigaChat-3-Ultra"), answers("yandexgpt")
			f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})
			ctx := context.Background()

			resp, err := f.Complete(ctx, testRequest())
			if err != nil || resp.Model != "yandexgpt" {
				t.Fatalf("got %+v, %v; want the secondary's answer", resp, err)
			}
			resp, err = f.Complete(ctx, testRequest())
			if err != nil || resp.Model != "GigaChat-3-Ultra" || primary.callCount() != 2 {
				t.Errorf("next call: got %+v, %v, primary called %d times; want the primary again",
					resp, err, primary.callCount())
			}
		})
	}
}

// The caller's deadline or cancellation ends the call: there is no time left
// for another backend, and the one that was working is not to blame.
func TestFallbackStopsWhenCallerGivesUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	primary := &fakeProvider{reply: func(ctx context.Context, _ int) (Response, error) {
		cancel()
		return Response{}, fmt.Errorf("gigachat: %w", ctx.Err())
	}}
	secondary := answers("yandexgpt")
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})

	_, err := f.Complete(ctx, testRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if secondary.callCount() != 0 {
		t.Errorf("secondary called %d times, want 0", secondary.callCount())
	}

	if _, err := f.Complete(context.Background(), testRequest()); primary.callCount() != 2 {
		t.Errorf("next call: primary called %d times (%v), want 2: not cooled down", primary.callCount(), err)
	}
}

// A backend that hangs, as a provider under attack does, never returns an
// error by itself. Its own timeout cuts it off in time for the next backend
// and counts as unavailable.
func TestFallbackBackendTimeoutCountsAsUnavailable(t *testing.T) {
	primary := &fakeProvider{reply: func(ctx context.Context, _ int) (Response, error) {
		<-ctx.Done()
		return Response{}, fmt.Errorf("gigachat: chat request: %w", ctx.Err())
	}}
	secondary := answers("yandexgpt")
	f, _ := newTestFallback(
		Backend{Name: "gigachat", Provider: primary, Timeout: 20 * time.Millisecond},
		Backend{Name: "yandexgpt", Provider: secondary},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := f.Complete(ctx, testRequest())
	if err != nil || resp.Model != "yandexgpt" {
		t.Fatalf("got %+v, %v; want the secondary's answer", resp, err)
	}

	if _, err := f.Complete(ctx, testRequest()); err != nil || primary.callCount() != 1 {
		t.Errorf("next call: %v, primary called %d times; want it skipped", err, primary.callCount())
	}
}

// When every backend fails, the caller sees each error: a refusal anywhere
// still reads as ErrRefused.
func TestFallbackReturnsEveryErrorWhenAllFail(t *testing.T) {
	primary, secondary := fails(errDown), fails(errRefused)
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})

	_, err := f.Complete(context.Background(), testRequest())
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrRefused) {
		t.Errorf("got %v, want both ErrUnavailable and ErrRefused", err)
	}
	if primary.callCount() != 1 || secondary.callCount() != 1 {
		t.Errorf("calls: primary %d, secondary %d; want one each", primary.callCount(), secondary.callCount())
	}
}

// With every backend cooling down, trying them beats failing outright; the
// one that answers is back in service at once.
func TestFallbackTriesCooledDownBackendsWhenAllAreDown(t *testing.T) {
	primary, secondary := failsFirst(errDown, "GigaChat-3-Ultra"), fails(errDown)
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})
	ctx := context.Background()

	if _, err := f.Complete(ctx, testRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first call: got %v, want ErrUnavailable", err)
	}

	resp, err := f.Complete(ctx, testRequest())
	if err != nil || resp.Model != "GigaChat-3-Ultra" {
		t.Fatalf("all down: got %+v, %v; want the primary tried and answering", resp, err)
	}
	if secondary.callCount() != 1 {
		t.Errorf("all down: secondary called %d times, want 1: the primary answered first", secondary.callCount())
	}

	if _, err := f.Complete(ctx, testRequest()); err != nil || primary.callCount() != 3 {
		t.Errorf("after recovery: %v, primary called %d times; want it first in line again", err, primary.callCount())
	}
}

func TestFallbackIsSafeForConcurrentUse(t *testing.T) {
	primary, secondary := fails(errDown), answers("yandexgpt")
	f, _ := newTestFallback(Backend{Name: "gigachat", Provider: primary}, Backend{Name: "yandexgpt", Provider: secondary})

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			if _, err := f.Complete(context.Background(), testRequest()); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent call: %v", err)
	}
}

// A backend whose timeout fires while the request still waits for a slot is
// busy with our own requests, not down: the next backend takes this one, and
// the busy one stays first in line.
func TestFallbackWaitingForASlotIsNotUnavailable(t *testing.T) {
	release := make(chan struct{})
	slow := &fakeProvider{reply: func(_ context.Context, call int) (Response, error) {
		if call == 1 {
			<-release
		}
		return answer("GigaChat-3-Ultra")
	}}
	primary := Limit(slow, 1)
	secondary := answers("yandexgpt")
	f, _ := newTestFallback(
		Backend{Name: "gigachat", Provider: primary, Timeout: 20 * time.Millisecond},
		Backend{Name: "yandexgpt", Provider: secondary},
	)

	busy := make(chan struct{})
	go func() {
		defer close(busy)
		_, _ = primary.Complete(context.Background(), testRequest())
	}()
	for slow.callCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	resp, err := f.Complete(context.Background(), testRequest())
	if err != nil || resp.Model != "yandexgpt" {
		t.Fatalf("got %+v, %v; want the secondary's answer", resp, err)
	}
	close(release)
	<-busy

	resp, err = f.Complete(context.Background(), testRequest())
	if err != nil || resp.Model != "GigaChat-3-Ultra" {
		t.Errorf("next call: got %+v, %v; want the primary, not cooled down", resp, err)
	}
}

func TestNativePDFNeverFallsBackOrUsesLegacyPrimary(t *testing.T) {
	legacy := answers("legacy")
	router := fails(ErrUnavailable)
	f := NewFallback(time.Minute, Backend{Name: "gigachat", Provider: legacy}, Backend{Name: "openrouter", Provider: router})
	_, err := f.Complete(context.Background(), Request{PDFs: []PDF{{Name: "source.pdf"}}})
	if !errors.Is(err, ErrUnavailable) || legacy.callCount() != 0 || router.callCount() != 1 {
		t.Fatalf("legacy=%d router=%d err=%v", legacy.callCount(), router.callCount(), err)
	}
	f = NewFallback(time.Minute, Backend{Name: "gigachat", Provider: legacy})
	if _, err = f.Complete(context.Background(), Request{PDFs: []PDF{{Name: "source.pdf"}}}); !errors.Is(err, ErrCapability) || legacy.callCount() != 0 {
		t.Fatalf("unsupported native source: %v", err)
	}
}

func TestGenerationCannotSilentlySwitchPaidProviders(t *testing.T) {
	primary, backup := fails(ErrUnavailable), answers("backup")
	f := NewFallback(time.Minute, Backend{Name: "primary", Provider: primary}, Backend{Name: "backup", Provider: backup})
	_, err := f.Complete(context.Background(), Request{NoFallback: true, Source: "complete source"})
	if !errors.Is(err, ErrUnavailable) || primary.callCount() != 1 || backup.callCount() != 0 {
		t.Fatalf("primary=%d backup=%d err=%v", primary.callCount(), backup.callCount(), err)
	}
}
