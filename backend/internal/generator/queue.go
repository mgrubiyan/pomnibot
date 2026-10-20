package generator

import (
	"context"
	"sync"
)

// Queue lets a fixed number of documents through at a time; the rest wait in
// arrival order. Reading a document and turning it into cards both call the
// model, so the number of slots is how many requests the model serves at once.
type Queue struct {
	slots   chan struct{}
	mu      sync.Mutex
	waiting int
}

// NewQueue makes a queue with size slots, at least one.
func NewQueue(size int) *Queue {
	return &Queue{slots: make(chan struct{}, max(size, 1))}
}

// Acquire takes a slot. When none is free it calls onWait with the number of
// documents ahead, then blocks until a slot frees or ctx ends. The returned
// release must be called once the document is done.
func (q *Queue) Acquire(ctx context.Context, onWait func(ahead int)) (release func(), err error) {
	release = func() { <-q.slots }

	q.mu.Lock()
	// A newcomer takes a free slot only when nobody waits: a waiter may still
	// be in onWait, sending its queue message, when the slot frees.
	if q.waiting == 0 {
		select {
		case q.slots <- struct{}{}:
			q.mu.Unlock()
			return release, nil
		default:
		}
	}
	ahead := cap(q.slots) + q.waiting
	q.waiting++
	q.mu.Unlock()

	defer func() {
		q.mu.Lock()
		q.waiting--
		q.mu.Unlock()
	}()

	if onWait != nil {
		onWait(ahead)
	}
	// A freed slot goes to the longest-blocked sender, so waiters start in
	// arrival order.
	select {
	case q.slots <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
