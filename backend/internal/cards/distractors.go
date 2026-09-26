package cards

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Distractors for choice cards are answers to cards from other fragments of
// the same document. The model never writes wrong options: an invented one
// could turn out true, or teach something that is not in the notes.
//
// Progressive delivery makes this a waiting game: the first fragment has no
// neighbours yet. A choice card therefore waits in a pending queue while new
// fragments add answers to the pool, and is released as soon as it gets
// wantDistractors options. After maxPendingWait more fragments it settles for
// minDistractors, and with fewer it becomes a flip card instead of being
// dropped. At the end of the document everything still pending is settled
// the same way. So cards from the first fragments arrive a few fragments
// late, and the rest of the feed is not held back by them.
const (
	wantDistractors = 3
	minDistractors  = 2
	maxPendingWait  = 3 // fragments a choice card waits for better options
	// maxLenRatio bounds how much longer or shorter a distractor may be than
	// the answer: next to "митоз", "совокупность процессов между делениями
	// клетки" gives itself away.
	maxLenRatio = 3.0
	// maxInputWords is the longest answer still reasonable to type; longer
	// input cards become flip cards.
	maxInputWords = 4
)

type poolEntry struct {
	answer   string
	key      string // folded answer for comparisons
	topic    string // folded
	pos      int    // fragment position among those sent to the model
	runes    int
	hasDigit bool
}

// distractorPool collects answers from all accepted and budget-trimmed cards.
type distractorPool struct {
	entries []poolEntry
	seen    map[string]bool
}

func newDistractorPool() *distractorPool {
	return &distractorPool{seen: map[string]bool{}}
}

func (p *distractorPool) add(c modelCard, pos int) {
	if c.Kind == KindBoolean {
		return
	}
	key := answerKey(c.Answer)
	if key == "" || p.seen[key] {
		return
	}
	p.seen[key] = true
	p.entries = append(p.entries, poolEntry{
		answer:   c.Answer,
		key:      key,
		topic:    answerKey(c.Topic),
		pos:      pos,
		runes:    utf8.RuneCountInString(key),
		hasDigit: strings.IndexFunc(key, unicode.IsDigit) >= 0,
	})
}

// pick returns up to n distractors for a card from fragment pos. A candidate
// must come from another fragment, differ from the answer, not contain it or
// be contained in it, not appear in the question or in the card's quote (it
// is likely correct too), have digits exactly when the answer has them, and
// be of comparable length. Among those, same-topic and closer-length answers
// win; ties go to nearer fragments, then alphabetical order, so the choice is
// deterministic.
func (p *distractorPool) pick(c Card, pos, n int) []string {
	answer := answerKey(c.Answer)
	question := fold(c.Question).s
	quote := fold(c.SourceQuote).s
	topic := answerKey(c.Topic)
	runes := utf8.RuneCountInString(answer)
	hasDigit := strings.IndexFunc(answer, unicode.IsDigit) >= 0
	if runes == 0 {
		return nil
	}

	type scored struct {
		e     poolEntry
		score float64
		dist  int
	}
	var cands []scored
	for _, e := range p.entries {
		if e.pos == pos || e.hasDigit != hasDigit {
			continue
		}
		if strings.Contains(e.key, answer) || strings.Contains(answer, e.key) {
			continue
		}
		if containsPhrase(question, e.key) || containsPhrase(quote, e.key) {
			continue
		}
		ratio := float64(e.runes) / float64(runes)
		if ratio > maxLenRatio || ratio < 1/maxLenRatio {
			continue
		}
		score := -math.Abs(math.Log(ratio))
		if topic != "" && e.topic == topic {
			score++
		}
		dist := e.pos - pos
		if dist < 0 {
			dist = -dist
		}
		cands = append(cands, scored{e, score, dist})
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		return a.e.key < b.e.key
	})

	var out []string
	var keys []string
	for _, cand := range cands {
		if len(out) == n {
			break
		}
		if overlapsAny(cand.e.key, keys) {
			continue
		}
		out = append(out, cand.e.answer)
		keys = append(keys, cand.e.key)
	}
	return out
}

// overlapsAny rejects "мейоз I" next to "мейоз": two options where one
// contains the other.
func overlapsAny(key string, keys []string) bool {
	for _, k := range keys {
		if strings.Contains(k, key) || strings.Contains(key, k) {
			return true
		}
	}
	return false
}

// containsPhrase is strings.Contains on word boundaries: "ион" is not in
// "функционирование".
func containsPhrase(s, phrase string) bool {
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], phrase)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(phrase)
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[end:])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(s[start:])
		from = start + size
	}
	return false
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func answerKey(s string) string {
	return trimQuote(fold(s).s)
}

// shuffleOptions puts the answer among the distractors in an order derived
// from the question: the same card always looks the same, and the right
// answer is not always first.
func shuffleOptions(question, answer string, distractors []string) []string {
	options := append([]string{answer}, distractors...)
	h := fnv.New64a()
	_, _ = h.Write([]byte(fold(question).s))
	state := h.Sum64()
	for i := len(options) - 1; i > 0; i-- {
		state = splitmix64(state)
		j := int(state % uint64(i+1))
		options[i], options[j] = options[j], options[i]
	}
	return options
}

// splitmix64 is a tiny PRNG step. Written out rather than math/rand so the
// order stays the same across Go versions.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
