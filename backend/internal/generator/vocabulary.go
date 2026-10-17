package generator

import (
	"regexp"
	"strings"
)

// maxListFacts caps facts per fragment for a vocabulary list. A word with its
// translation takes about 300 tokens of cards, so 15 fit the answer budget.
const maxListFacts = 15

// reTermLine is a line of a vocabulary list or glossary: an optional number,
// a short term, a separator and its meaning: «5 to configure [kən'fɪɡə] –
// настраивать», «seamless: непрерывный», a table row «Митоз | непрямое
// деление», a tab between, or a reaction «CaCO₃ → CaO + CO₂», a fact a line
// too. Not «=»: «sin 30° = 1/2» lines are formulas, often a fragment full.
var reTermLine = regexp.MustCompile(`^\s*(?:\d{1,3}[.)]?\s+)?[^–—:=|→\t\n]{1,80}?(?:\s[–—-]\s+|\s*[:|→]\s*|\t+)[^=\n]{1,150}$`)

// reBracketed is a transcription or a note in brackets, «['dɔ:ntɪŋ]»: it may
// hold a colon that is no separator.
var reBracketed = regexp.MustCompile(`\[[^\]\n]*\]`)

// isTermLine reports a line of a vocabulary list or glossary.
func isTermLine(line string) bool {
	return reTermLine.MatchString(reBracketed.ReplaceAllString(line, ""))
}

// factCap is how many facts the model may return for a fragment. A
// vocabulary list holds a fact in every line, far more than prose, so the
// cap rises to the number of term lines, up to maxListFacts; otherwise it is
// base.
func factCap(fragment string, base int) int {
	return max(base, min(termLines(strings.Split(fragment, "\n")), maxListFacts))
}

// termLines counts the lines of a vocabulary list or glossary.
func termLines(lines []string) int {
	n := 0
	for _, line := range lines {
		if isTermLine(line) {
			n++
		}
	}
	return n
}
