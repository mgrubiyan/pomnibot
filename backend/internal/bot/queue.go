package bot

import (
	"context"
	"sync"
)

// generationSlots is how many documents are read and turned into cards at
// once by default. Both steps call the model, and the GigaChat freemium plan
// serves exactly one request at a time; a second one gets 429. A model with
// more room raises it with Bot.SetGenerationSlots.
const generationSlots = 1

// generationQueue lets a fixed number of documents through at a time; the
// rest wait in arrival order.
type generationQueue struct {
	slots   chan struct{}
	mu      sync.Mutex
	waiting int
}

func newGenerationQueue(size int) *generationQueue {
	return &generationQueue{slots: make(chan struct{}, size)}
}

// acquire takes a slot. When none is free it calls onWait with the number of
// documents ahead, then blocks until a slot frees or ctx ends. The returned
// release must be called once the document is done.
func (q *generationQueue) acquire(ctx context.Context, onWait func(ahead int)) (release func(), err error) {
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
