package cards

import (
	"strings"
	"unicode"
)

// Duplicate detection. Overlapping fragments and repeated material make the
// model ask the same thing twice in different words. Two questions are the
// same when their normalized text matches or when the Jaccard similarity of
// their content-word stems reaches dupThreshold. The first card is kept.
const (
	// dupThreshold 0.75 catches "Какие фазы выделяют в митозе?" against
	// "Какие фазы выделяют в митозе клетки?" (3 of 4 stems) but keeps
	// "Что такое митоз?" and "Что такое мейоз?" apart.
	dupThreshold = 0.75
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
		if jaccard(stems, seen) >= dupThreshold {
			return true
		}
	}
	return false
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

func jaccard(a, b map[string]bool) float64 {
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	union := len(a) + len(b) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}
