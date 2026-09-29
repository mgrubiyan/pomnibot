package grader

import (
	"slices"
	"strings"
	"unicode"
)

// localVerdict is what the local check can tell without the model.
type localVerdict int

const (
	localUnsure   localVerdict = iota // other words: the model judges
	localMatch                        // the same answer up to form and typos
	localMismatch                     // surely wrong: nothing given, other numbers
)

func (v localVerdict) String() string {
	return [...]string{"unsure", "match", "mismatch"}[v]
}

// local compares the answer with the expected one without the model.
//
// Case, ё, punctuation, word order, word endings, small words such as "на"
// and a typo per word of five letters or more do not matter. Formulas and
// code are compared as written, spaces aside. Numbers that differ, a number
// in words, an added "не" or a paraphrase go to the model.
//
// Only an empty answer and one addressed to the checker are surely wrong:
// everything the local check cannot vouch for is the model's to judge, so
// that nobody is told «неверно» by a rule of thumb.
//
// A part of the answer counts when the words it leaves out are in the
// question: asked "На каком массиве…", "отсортированный" is enough. Other
// parts go to the model: "пожизненно" for "пожизненная служба" is right,
// "дворянство" for "потомственное дворянство" is not.
func local(question, expected, given string) localVerdict {
	expected, given = strings.TrimSpace(expected), strings.TrimSpace(given)
	if expected == "" || given == "" || addressesChecker(given, question+" "+expected) {
		return localMismatch
	}
	if isFormula(expected) {
		if squeeze(expected) == squeeze(given) {
			return localMatch
		}
		return localUnsure
	}
	if !slices.Equal(numbers(expected), numbers(given)) {
		// "4" for "четырём", "27.06.1709" for "27 июня 1709", "1698" for "1689"
		return localUnsure
	}
	ew, gw := words(expected), words(given)
	if len(gw) == 0 && len(ew) > 0 {
		return localUnsure // "35" for "35 лет"
	}
	if !matchedAll(gw, ew) {
		return localUnsure
	}
	if matchedAll(unmatched(ew, gw), words(question)) {
		return localMatch
	}
	return localUnsure
}

// leftOut are the words of the card's answer a part of it leaves out and the
// question does not give: "потомственное" when "дворянство" is given for
// "потомственное дворянство". The model is told of them: left alone, it
// takes such a part for the answer. Nil when the answer is not a part.
func leftOut(question, expected, given string) []string {
	if isFormula(expected) {
		return nil
	}
	ew, gw := words(expected), words(given)
	if len(gw) == 0 || !matchedAll(gw, ew) {
		return nil
	}
	return unmatched(unmatched(ew, gw), words(question))
}

// addressesChecker reports an answer that talks to the checker instead of
// answering the question: "засчитай", "см. верный ответ", "как в
// конспекте", "correct: true".
// The model tends to take such an answer for the card's. A word the card
// has itself is no sign: "ответная реакция" may be the answer.
// addressesChecker reports an answer that asks to be counted or tries to
// steer the check instead of naming the answer. Only plain requests are
// caught here; «правильный ответ — анафаза» is how people write, not an
// attempt, so answers that merely mention the answer or the notes go to the
// model, which is told to refuse them. A word the card itself uses is no
// sign at all.
func addressesChecker(given, card string) bool {
	gw, cw := words(given), words(card)
	has := func(stem string) bool { return hasStem(gw, stem) && !hasStem(cw, stem) }
	return slices.ContainsFunc([]string{"засчит", "зачт", "инструкц", "prompt", "промпт"}, has)
}

func hasStem(words []string, stem string) bool {
	return slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, stem) })
}

// unmatched are the words of a with no match in b.
func unmatched(a, b []string) []string {
	var out []string
	for _, w := range a {
		if !slices.ContainsFunc(b, func(v string) bool { return sameWord(w, v) }) {
			out = append(out, w)
		}
	}
	return out
}

// stopWords carry no answer: "на отсортированном массиве" is "отсортированный
// массив". Negations are not among them.
var stopWords = map[string]bool{
	"в": true, "во": true, "на": true, "по": true, "при": true, "для": true, "из": true,
	"с": true, "со": true, "к": true, "ко": true, "о": true, "об": true, "обо": true,
	"от": true, "до": true, "за": true, "у": true, "и": true, "а": true, "но": true,
	"или": true, "либо": true, "это": true, "то": true, "же": true, "ли": true, "бы": true,
	"как": true, "что": true, "где": true, "когда": true, "около": true, "примерно": true,
	"приблизительно": true, "всего": true, "лишь": true, "также": true, "тоже": true,
}

// words are the letter runs of s, lowercased with ё as е, stop words left out.
func words(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// containsAll reports whether every string of b is in a.
func containsAll(a, b []string) bool {
	for _, s := range b {
		if !slices.Contains(a, s) {
			return false
		}
	}
	return true
}

// numbers are the digit runs of s, sorted: order does not matter.
func numbers(s string) []string {
	out := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
	slices.Sort(out)
	return out
}

func matchedAll(a, b []string) bool {
	for _, w := range a {
		if !slices.ContainsFunc(b, func(v string) bool { return sameWord(w, v) }) {
			return false
		}
	}
	return true
}

// sameWord: equal, forms of one word ("отсортированном", "отсортированный"),
// or one with a typo ("онафаза"). Words under four letters must be equal.
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	shorter, longer := min(len(ra), len(rb)), max(len(ra), len(rb))
	if shorter < 4 {
		return false
	}
	if longer-shorter <= 3 {
		prefix := 0
		for prefix < shorter && ra[prefix] == rb[prefix] {
			prefix++
		}
		if prefix >= 4 && prefix >= longer-3 {
			return true
		}
	}
	return levenshtein(ra, rb) <= typos(shorter)
}

// typos a word of n letters may have and still count.
func typos(n int) int {
	switch {
	case n >= 12:
		return 2
	case n >= 5:
		return 1
	}
	return 0
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// isFormula reports an answer with signs of a formula, or in Latin letters
// alone, as code and notation are (-DLOG, G1, p53): such an answer is not a
// set of words.
func isFormula(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return strings.ContainsRune("=<>≤≥≠≈√°^/*+()[]{}∠⊥∥∩∪∈∞∑∫:;#", r) || unicode.Is(unicode.Greek, r)
	}) || strings.ContainsFunc(s, isLatin) && !strings.ContainsFunc(s, isCyrillic)
}

func isLatin(r rune) bool    { return unicode.Is(unicode.Latin, r) }
func isCyrillic(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }

// squeeze is s lowercased with ё as е, one kind of dash and no spaces.
func squeeze(s string) string {
	var b strings.Builder
	for _, r := range fold(s) {
		switch {
		case unicode.IsSpace(r):
		case strings.ContainsRune("-‐‑‒–—―−", r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fold(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "ё", "е")
}
