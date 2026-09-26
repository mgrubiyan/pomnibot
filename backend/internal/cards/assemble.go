package cards

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// chunkResult is what a worker returns for one fragment. Quotes are already
// checked: that needs only the fragment itself.
type chunkResult struct {
	pos       int // position among fragments sent to the model
	chunk     chunk
	cards     []modelCard // quote found, Quote replaced with the span of the notes
	holdsSlot bool        // a concurrency slot the assembler must release
	skipped   bool        // budget used up before the call
	cancelled bool
	invalid   bool // no parseable answer after the retry
	err       error

	calls            int
	invalidResponses int
	cardsFromModel   int
	droppedInvalid   int
	droppedQuote     int
	usage            Usage
	models           map[string]int
	elapsed          time.Duration
}

type pendingChoice struct {
	card Card
	pos  int
	seq  int
}

// reserved is a verified card that did not fit its fragment's share of
// MaxCardsPerDoc. It may still get in at the end if later fragments leave
// part of the budget unused.
type reserved struct {
	card  modelCard
	pos   int
	index int // chunk.Index, for SourceRef
}

type placedCard struct {
	card Card
	pos  int
	seq  int
}

// assembler turns fragment results into final cards. It sees results
// strictly in fragment order, which makes "keep the first duplicate" and the
// per-fragment budget well defined no matter how many calls run in parallel.
// It runs in one goroutine and owns all its state; only accepted is read from
// the dispatcher.
type assembler struct {
	title    string
	work     int // fragments sent to the model, the budget is spread over them
	maxDoc   int
	stats    *Stats
	accepted atomic.Int64 // cards counted against MaxCardsPerDoc, pending ones included

	questions *questionIndex
	pool      *distractorPool
	pending   []pendingChoice
	reserve   []reserved
	placed    []placedCard
	seq       int

	timeSum   time.Duration
	timeCount int
}

func newAssembler(title string, work, maxDoc int, stats *Stats) *assembler {
	return &assembler{
		title:     title,
		work:      work,
		maxDoc:    maxDoc,
		stats:     stats,
		questions: newQuestionIndex(),
		pool:      newDistractorPool(),
	}
}

// quota is how many cards fragments 0..pos may have together: MaxCardsPerDoc
// spread evenly over the document. What a fragment leaves unused carries
// over to the next ones, so an empty fragment does not shrink the total, and
// a long document gets cards from its end, not only its first N.
func (a *assembler) quota(pos int) int {
	if a.maxDoc == 0 {
		return int(^uint(0) >> 1)
	}
	return (pos + 1) * a.maxDoc / a.work
}

// budgetSpent reports whether fragment pos can get no cards whatever the
// fragments before it return, so the model call can be skipped. accepted only
// grows, so a stale read errs towards making the call.
func (a *assembler) budgetSpent(pos int) bool {
	return a.maxDoc > 0 && a.quota(pos) <= int(a.accepted.Load())
}

// add takes the next fragment result in order and returns cards ready to
// deliver.
func (a *assembler) add(r chunkResult) []Card {
	a.count(r)

	allowance := a.quota(r.pos) - int(a.accepted.Load())
	var ready []Card
	for _, mc := range r.cards {
		if a.questions.isDup(mc.Question) {
			a.stats.DroppedDuplicate++
			continue
		}
		// Trimmed cards still lend their answers as distractors: the text
		// is verified, it just does not fit in the budget.
		a.pool.add(mc, r.pos)
		if allowance <= 0 {
			a.stats.DroppedByLimit++
			a.reserve = append(a.reserve, reserved{card: mc, pos: r.pos, index: r.chunk.Index})
			continue
		}
		allowance--
		if c, ok := a.accept(mc, r.pos, r.chunk.Index); ok {
			ready = append(ready, c)
		}
	}
	return append(ready, a.settle(r.pos, false)...)
}

// accept counts a card against the budget and either places it or, for a
// choice card, queues it for distractors. It returns the card and true when
// it is ready to deliver.
func (a *assembler) accept(mc modelCard, pos, index int) (Card, bool) {
	a.accepted.Add(1)
	a.questions.add(mc.Question)
	card := Card{
		Kind:        mc.Kind,
		Question:    mc.Question,
		Answer:      mc.Answer,
		Explanation: mc.Explanation,
		SourceQuote: mc.Quote,
		SourceRef:   sourceRef(a.title, index),
		Topic:       mc.Topic,
	}
	a.seq++
	switch {
	case card.Kind == KindChoice:
		a.pending = append(a.pending, pendingChoice{card: card, pos: pos, seq: a.seq})
		return Card{}, false
	case card.Kind == KindInput && len(strings.Fields(card.Answer)) > maxInputWords:
		card.Kind = KindFlip
		a.stats.InputToFlip++
	}
	return a.place(card, pos, a.seq), true
}

// finish fills what is left of the budget from the reserve and settles
// choice cards still waiting for distractors.
func (a *assembler) finish() []Card {
	ready := a.backfill()
	return append(ready, a.settle(a.work, true)...)
}

// backfill spends budget that fragments left unused (few or no cards in the
// last ones) on cards trimmed earlier. It takes one card per fragment in
// turn, so the spread stays even rather than favouring the first fragment.
func (a *assembler) backfill() []Card {
	room := a.maxDoc - int(a.accepted.Load())
	if a.maxDoc == 0 || room <= 0 || len(a.reserve) == 0 {
		return nil
	}
	var byPos [][]reserved
	for _, r := range a.reserve {
		if n := len(byPos); n == 0 || byPos[n-1][0].pos != r.pos {
			byPos = append(byPos, nil)
		}
		byPos[len(byPos)-1] = append(byPos[len(byPos)-1], r)
	}

	var ready []Card
	for round := 0; room > 0; round++ {
		took := false
		for _, group := range byPos {
			if room == 0 {
				break
			}
			if round >= len(group) {
				continue
			}
			took = true
			r := group[round]
			a.stats.DroppedByLimit--
			if a.questions.isDup(r.card.Question) {
				a.stats.DroppedDuplicate++ // repeats a card accepted after it was trimmed
				continue
			}
			room--
			if c, ok := a.accept(r.card, r.pos, r.index); ok {
				ready = append(ready, c)
			}
		}
		if !took {
			break
		}
	}
	a.reserve = nil
	return ready
}

// settle gives pending choice cards their options. A card is released with
// wantDistractors options as soon as the pool has them; once it has waited
// maxPendingWait fragments, or at the end, minDistractors will do, and with
// fewer it becomes a flip card.
func (a *assembler) settle(pos int, final bool) []Card {
	var ready []Card
	kept := a.pending[:0]
	for _, p := range a.pending {
		options := a.pool.pick(p.card, p.pos, wantDistractors)
		timeUp := final || pos-p.pos >= maxPendingWait
		switch {
		case len(options) >= wantDistractors || (timeUp && len(options) >= minDistractors):
			answer := optionForm(p.card.Answer, startsUpper(p.card.Answer))
			for i := range options {
				options[i] = optionForm(options[i], startsUpper(answer))
			}
			p.card.Answer = answer
			p.card.Options = shuffleOptions(p.card.Question, answer, options)
		case timeUp:
			p.card.Kind = KindFlip
			a.stats.DowngradedToFlip++
		default:
			kept = append(kept, p)
			continue
		}
		ready = append(ready, a.place(p.card, p.pos, p.seq))
	}
	a.pending = kept
	return ready
}

func (a *assembler) place(c Card, pos, seq int) Card {
	a.placed = append(a.placed, placedCard{card: c, pos: pos, seq: seq})
	return c
}

func (a *assembler) count(r chunkResult) {
	s := a.stats
	switch {
	case r.skipped:
		s.ChunksSkipped++
		return
	case r.cancelled:
	case r.err != nil:
		s.ChunksFailed++
	case r.invalid:
		s.ChunksInvalid++
	}
	s.ModelCalls += r.calls
	s.InvalidResponses += r.invalidResponses
	s.PromptTokens += r.usage.PromptTokens
	s.CompletionTokens += r.usage.CompletionTokens
	for m, n := range r.models {
		s.CallsByModel[m] += n
	}
	s.CardsFromModel += r.cardsFromModel
	s.DroppedInvalid += r.droppedInvalid
	s.DroppedQuote += r.droppedQuote

	if r.calls > 0 {
		a.timeSum += r.elapsed
		a.timeCount++
		s.ChunkTimeMax = max(s.ChunkTimeMax, r.elapsed)
		s.ChunkTimeAvg = a.timeSum / time.Duration(a.timeCount)
	}
}

// cards returns every delivered card in document order.
func (a *assembler) cards() []Card {
	sort.SliceStable(a.placed, func(i, j int) bool {
		if a.placed[i].pos != a.placed[j].pos {
			return a.placed[i].pos < a.placed[j].pos
		}
		return a.placed[i].seq < a.placed[j].seq
	})
	out := make([]Card, len(a.placed))
	for i, p := range a.placed {
		out[i] = p.card
	}
	return out
}

func sourceRef(title string, index int) string {
	if title == "" {
		return fmt.Sprintf("Фрагмент %d", index+1)
	}
	return fmt.Sprintf("%s, фрагмент %d", title, index+1)
}
