package cards

import (
	"strings"
	"unicode"
)

// Duplicate detection. Overlapping fragments and repeated material make the
// model ask the same thing twice in slightly different words. The first card
// is kept.
//
// Two questions are the same when their normalized text matches, when they
// share nearly all stems (sameJaccard), or when one is the other plus at most
// maxExtraStems content words and they share at least minCommonStems
// ("Какие фазы выделяют в митозе?" and "…в митозе клетки?"; but not "Что такое
// хромосома?" and "Что такое гомологичная хромосома?"). A word swapped for another is what tells questions apart:
// "…в профазе первого деления мейоза?" and "…второго деления мейоза?" are two
// cards, though they share 6 stems of 8.
const (
	maxExtraStems  = 1
	minCommonStems = 2
	sameJaccard    = 0.9
	// stemRunes is a poor man's Russian stemmer: "митоза", "митозом" and
	// "митоз" share their first five letters.
	stemRunes = 5
)

// Question words and function words: "Что такое митоз?" and "Что называют
// митозом?" ask the same thing.
var stopWords = map[string]bool{
	"что": true, "как": true, "какой": true, "какая": true, "какое": true, "какие": true,
	"каков": true, "какова": true, "каково": true, "каковы": true, "каким": true, "какую": true,
	"кто": true, "где": true, "когда": true, "почему": true, "зачем": true, "сколько": true,
	"чем": true, "чего": true, "кого": true, "чему": true, "ли": true, "же": true,
	"это": true, "этот": true, "эта": true, "эти": true, "такое": true, "такой": true,
	"называется": true, "называют": true, "называются": true, "является": true, "являются": true,
	"в": true, "во": true, "на": true, "и": true, "а": true, "но": true, "или": true,
	"с": true, "со": true, "к": true, "ко": true, "по": true, "о": true, "об": true,
	"от": true, "до": true, "из": true, "у": true, "за": true, "для": true, "при": true,
	"под": true, "над": true, "не": true, "ни": true, "то": true,
}

type questionIndex struct {
	exact map[string]bool
	stems []map[string]bool
}

func newQuestionIndex() *questionIndex {
	return &questionIndex{exact: map[string]bool{}}
}

// isDup reports whether q repeats a question already added.
func (x *questionIndex) isDup(q string) bool {
	words := questionWords(q)
	if x.exact[strings.Join(words, " ")] {
		return true
	}
	stems := stemSet(words)
	if len(stems) == 0 {
		return false
	}
	for _, seen := range x.stems {
		if sameQuestion(stems, seen) {
			return true
		}
	}
	return false
}

func sameQuestion(a, b map[string]bool) bool {
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	onlyA, onlyB := len(a)-common, len(b)-common
	if common >= minCommonStems && min(onlyA, onlyB) == 0 && max(onlyA, onlyB) <= maxExtraStems {
		return true
	}
	return float64(common)/float64(common+onlyA+onlyB) >= sameJaccard
}

func (x *questionIndex) add(q string) {
	words := questionWords(q)
	x.exact[strings.Join(words, " ")] = true
	if stems := stemSet(words); len(stems) > 0 {
		x.stems = append(x.stems, stems)
	}
}

// questionWords folds q and splits it into words, dropping punctuation.
func questionWords(q string) []string {
	return strings.FieldsFunc(fold(q).s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func stemSet(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		if stopWords[w] {
			continue
		}
		if r := []rune(w); len(r) > stemRunes {
			w = string(r[:stemRunes])
		}
		set[w] = true
	}
	return set
}
