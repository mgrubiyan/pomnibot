package generator

import (
	"regexp"
	"strings"
)

// maxListFacts caps facts per fragment for a vocabulary list. A word with its
// translation takes about 300 tokens of cards, so 15 fit the answer budget.
const maxListFacts = 15

// reTermLine is a line of a vocabulary list or glossary: an optional number,
// a short term, a dash and its meaning, «5 to configure [kən'fɪɡə] –
// настраивать».
var reTermLine = regexp.MustCompile(`^\s*(?:\d{1,3}[.)]?\s+)?[^–—\n]{1,80}?\s[–—-]\s+\S.{0,150}$`)

// factCap is how many facts the model may return for a fragment. A
// vocabulary list holds a fact in every line, far more than prose, so the
// cap rises to the number of term lines, up to maxListFacts; otherwise it is
// base.
func factCap(fragment string, base int) int {
	terms := 0
	for _, line := range strings.Split(fragment, "\n") {
		if reTermLine.MatchString(line) {
			terms++
		}
	}
	return max(base, min(terms, maxListFacts))
}
