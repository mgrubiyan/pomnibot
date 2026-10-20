package generator

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueue_FreeSlotDoesNotWait(t *testing.T) {
	q := NewQueue(1)

	release, err := q.Acquire(context.Background(), func(int) {
		t.Error("onWait called while a slot was free")
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
}

func TestQueue_ReportsDocumentsAhead(t *testing.T) {
	q := NewQueue(1)
	ctx := context.Background()

	releaseFirst, err := q.Acquire(ctx, nil)
	if err != nil {
		t.Fatalf("Acquire first: %v", err)
	}

	aheads := make(chan int, 2)
	acquired := make(chan func(), 2)
	for range 2 {
		waiting := make(chan struct{})
		go func() {
			release, err := q.Acquire(ctx, func(ahead int) {
				aheads <- ahead
				close(waiting)
			})
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			acquired <- release
		}()
		// Wait for this one to queue before starting the next, so the
		// positions are deterministic.
		<-waiting
	}

	if got := <-aheads; got != 1 {
		t.Errorf("second document: ahead = %d, want 1", got)
	}
	if got := <-aheads; got != 2 {
		t.Errorf("third document: ahead = %d, want 2", got)
	}

	select {
	case <-acquired:
		t.Fatal("a waiting document got a slot before the first released it")
	case <-time.After(50 * time.Millisecond):
	}

	releaseFirst()
	(<-acquired)()
	(<-acquired)()
}

func TestQueue_CancelledWhileWaiting(t *testing.T) {
	q := NewQueue(1)
	release, err := q.Acquire(context.Background(), nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := q.Acquire(ctx, func(int) { cancel() })
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not return after ctx was cancelled")
	}

	// The cancelled document no longer counts as waiting.
	next, stop := context.WithCancel(context.Background())
	defer stop()
	got := make(chan int, 1)
	go func() {
		_, _ = q.Acquire(next, func(ahead int) { got <- ahead })
	}()
	if ahead := <-got; ahead != 1 {
		t.Errorf("ahead after a cancelled waiter = %d, want 1", ahead)
	}
}

func TestNewQueue_Slots(t *testing.T) {
	for _, tc := range []struct{ size, want int }{{1, 1}, {2, 2}, {0, 1}, {-3, 1}} {
		if got := cap(NewQueue(tc.size).slots); got != tc.want {
			t.Errorf("NewQueue(%d): slots = %d, want %d", tc.size, got, tc.want)
		}
	}
}
