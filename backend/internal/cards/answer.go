package cards

import "slices"

// Answer checks for choice and input cards, whose answer is a short term the
// student picks or types. The quote proves the fact, but not that the model
// took the right term from it: «Как называется период жизни клетки от одного
// деления до следующего, состоящий из интерфазы и собственно деления?» →
// «Интерфаза» came with a verbatim quote and a wrong answer. Two cheap checks
// on words catch such cards:
//
//   - every content word of the answer occurs in the quote, so the answer is
//     taken from the notes, not made up; this also drops an answer like
//     "input", the kind's name written in its place;
//   - the answer does not occur in the question: such a card either gives
//     itself away or, as above, names a term from its own wording.
//
// Words are compared up to their endings (sameWord), since the answer and the
// quote rarely share a grammatical case. Boolean and flip answers are
// sentences that rephrase the quote, so they are left to the quote check.

// Word matching: a word of shortWord letters or less must match exactly
// ("G1", "90", "ДНК"); a longer one may differ in the last endingRunes letters
// of the shorter word, with at least minCommonPrefix letters in common.
const (
	shortWord       = 3
	endingRunes     = 2
	minCommonPrefix = 4
)

func answerChecked(k Kind) bool {
	return k == KindChoice || k == KindInput
}

// answerSupported reports whether a choice or input answer passes both
// checks against its quote and question.
func answerSupported(c modelCard, quote string) bool {
	return stemsWithin(c.Answer, quote) && !stemsWithin(c.Answer, c.Question)
}

// stemsWithin reports whether every content word of s occurs in text, up to
// its ending. An s with no content words ("это", "не") is never within.
func stemsWithin(s, text string) bool {
	want := contentWords(s)
	if len(want) == 0 {
		return false
	}
	have := contentWords(text)
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h []rune) bool { return sameWord(w, h) }) {
			return false
		}
	}
	return true
}

func contentWords(s string) [][]rune {
	var out [][]rune
	for _, w := range questionWords(s) {
		if !stopWords[w] {
			out = append(out, []rune(w))
		}
	}
	return out
}

// sameWord reports whether a and b are forms of one word: «точки» and
// «точках», «интерфаза» and «интерфазы», but not «митоз» and «мейоз» or
// «контрольные» and «контролирует».
func sameWord(a, b []rune) bool {
	if len(a) <= shortWord || len(b) <= shortWord {
		return slices.Equal(a, b)
	}
	common := 0
	for common < min(len(a), len(b)) && a[common] == b[common] {
		common++
	}
	return common >= minCommonPrefix && common >= min(len(a), len(b))-endingRunes
}
