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
	facts     []modelFact // quote found: Quote is the span of the notes, ID is set
	holdsSlot bool        // a concurrency slot the assembler must release
	skipped   bool        // budget used up before the call
	cancelled bool
	invalid   bool // no parseable answer after the retry
	err       error

	calls              int
	invalidResponses   int
	factsFromModel     int
	cardsFromModel     int
	droppedInvalid     int
	droppedQuote       int
	droppedUnsupported int
	usage              Usage
	models             map[string]int
	elapsed            time.Duration
}

type pendingChoice struct {
	card Card
	pos  int
	seq  int
	// hasSiblings: other cards of the fact are delivered, so a choice card
	// left without distractors is dropped rather than turned into a flip.
	hasSiblings bool
}

// reserved is a verified fact that did not fit its fragment's share of
// MaxFactsPerDoc. It may still get in at the end if later fragments leave
// part of the budget unused.
type reserved struct {
	fact  modelFact
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
	accepted atomic.Int64 // facts counted against MaxFactsPerDoc

	facts     map[string]bool // ids of accepted facts
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
		facts:     map[string]bool{},
		questions: newQuestionIndex(),
		pool:      newDistractorPool(),
	}
}

// quota is how many facts fragments 0..pos may have together: MaxFactsPerDoc
// spread evenly over the document. What a fragment leaves unused carries
// over to the next ones, so an empty fragment does not shrink the total, and
// a long document gets facts from its end, not only its first N.
func (a *assembler) quota(pos int) int {
	if a.maxDoc == 0 {
		return int(^uint(0) >> 1)
	}
	return (pos + 1) * a.maxDoc / a.work
}

// budgetSpent reports whether fragment pos can get no facts whatever the
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
	for _, f := range r.facts {
		if a.facts[f.ID] {
			// The same quote again, typically from the overlap with the
			// previous fragment.
			a.stats.DroppedDuplicate += len(f.Cards)
			continue
		}
		// Trimmed facts still lend their answers as distractors: the text
		// is verified, it just does not fit in the budget.
		for _, mc := range f.Cards {
			a.pool.add(mc.Kind, mc.Answer, f.Topic, r.pos)
		}
		if allowance <= 0 {
			a.stats.DroppedByLimit += len(f.Cards)
			a.reserve = append(a.reserve, reserved{fact: f, pos: r.pos, index: r.chunk.Index})
			continue
		}
		if cards, ok := a.acceptFact(f, r.pos, r.chunk.Index); ok {
			allowance--
			ready = append(ready, cards...)
		}
	}
	return append(ready, a.settle(r.pos, false)...)
}

// acceptFact turns a fact's cards into Cards and counts the fact against the
// budget. Cards ready now are returned; choice cards wait in settle for
// distractors. ok is false when every card repeated an earlier question: the
// fact then costs nothing.
func (a *assembler) acceptFact(f modelFact, pos, index int) (ready []Card, ok bool) {
	var choices []pendingChoice
	var questions []string
	for _, mc := range f.Cards {
		if a.questions.isDup(mc.Question) {
			a.stats.DroppedDuplicate++
			continue
		}
		// Indexed after the loop: choice and input of one fact often share
		// the question, and that is not a duplicate.
		questions = append(questions, mc.Question)

		card := Card{
			FactID:      f.ID,
			Kind:        mc.Kind,
			Question:    mc.Question,
			Answer:      mc.Answer,
			Explanation: mc.Explanation,
			SourceQuote: f.Quote,
			SourceRef:   sourceRef(a.title, index),
			Topic:       f.Topic,
		}
		a.seq++
		switch {
		case card.Kind == KindChoice:
			choices = append(choices, pendingChoice{card: card, pos: pos, seq: a.seq})
			continue
		case card.Kind == KindInput && len(strings.Fields(card.Answer)) > maxInputWords:
			if len(f.Cards) > 1 {
				a.stats.DroppedVariants++
				continue
			}
			card.Kind = KindFlip
			a.stats.InputToFlip++
		}
		ready = append(ready, a.place(card, pos, a.seq))
	}
	for _, q := range questions {
		a.questions.add(q)
	}
	if len(ready) == 0 && len(choices) == 0 {
		return nil, false
	}

	a.facts[f.ID] = true
	a.accepted.Add(1)
	for i := range choices {
		choices[i].hasSiblings = len(ready) > 0
	}
	a.pending = append(a.pending, choices...)
	return ready, true
}

// finish fills what is left of the budget from the reserve and settles
// choice cards still waiting for distractors.
func (a *assembler) finish() []Card {
	ready := a.backfill()
	return append(ready, a.settle(a.work, true)...)
}

// backfill spends budget that fragments left unused (few or no facts in the
// last ones) on facts trimmed earlier. It takes one fact per fragment in
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
			a.stats.DroppedByLimit -= len(r.fact.Cards)
			if a.facts[r.fact.ID] {
				a.stats.DroppedDuplicate += len(r.fact.Cards) // accepted from a later fragment
				continue
			}
			if cards, ok := a.acceptFact(r.fact, r.pos, r.index); ok {
				room--
				ready = append(ready, cards...)
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
// maxPendingWait fragments, or at the end, minDistractors will do. With fewer
// it is dropped if other cards of its fact are out, and becomes a flip card
// if it is the fact's only one.
func (a *assembler) settle(pos int, final bool) []Card {
	var ready []Card
	kept := a.pending[:0]
	for _, p := range a.pending {
		options := a.pool.pick(p.card, p.pos, wantDistractors)
		timeUp := final || pos-p.pos >= maxPendingWait
		switch {
		case len(options) >= wantDistractors || (timeUp && len(options) >= minDistractors):
			answer := optionForm(p.card.Answer)
			for i := range options {
				options[i] = optionForm(options[i])
			}
			p.card.Answer = answer
			p.card.Options = shuffleOptions(p.card.Question, answer, options)
		case timeUp && p.hasSiblings:
			a.stats.DroppedVariants++
			continue
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
	s.FactsFromModel += r.factsFromModel
	s.CardsFromModel += r.cardsFromModel
	s.DroppedInvalid += r.droppedInvalid
	s.DroppedQuote += r.droppedQuote
	s.DroppedUnsupported += r.droppedUnsupported

	if r.calls > 0 {
		a.timeSum += r.elapsed
		a.timeCount++
		s.ChunkTimeMax = max(s.ChunkTimeMax, r.elapsed)
		s.ChunkTimeAvg = a.timeSum / time.Duration(a.timeCount)
	}
}

// cards returns every delivered card in document order, the cards of one
// fact next to each other.
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
