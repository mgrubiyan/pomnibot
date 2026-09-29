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
// and a typo per word of five letters or more do not matter. Numbers must
// match: "1689" is no typo for "1698". Formulas and code are compared as
// written, spaces aside. An added "не" or a paraphrase goes to the model.
//
// A part of the answer counts when the words it leaves out are in the
// question: asked "На каком массиве…", "отсортированный" is enough. A left
// out word the question does not give is the point of the answer, as
// "потомственное" in "потомственное дворянство": such a part is wrong.
func local(question, expected, given string) localVerdict {
	expected, given = strings.TrimSpace(expected), strings.TrimSpace(given)
	if expected == "" || given == "" {
		return localMismatch
	}
	if isFormula(expected) {
		if squeeze(expected) == squeeze(given) {
			return localMatch
		}
		return localUnsure
	}
	if !slices.Equal(numbers(expected), numbers(given)) {
		return localMismatch
	}
	ew, gw := words(expected), words(given)
	if matchedAll(gw, ew) {
		missing := unmatched(ew, gw)
		switch {
		case matchedAll(missing, words(question)):
			return localMatch
		case len(gw) > 0:
			return localMismatch
		}
	}
	return localUnsure
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

// isFormula reports an answer with signs of a formula or code: such an
// answer is not a set of words.
func isFormula(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return strings.ContainsRune("=<>≤≥≠≈√°^/*+()[]{}∠⊥∥∩∪∈∞∑∫:;#", r) || unicode.Is(unicode.Greek, r)
	})
}

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
