package cards

// Answer checks for choice and input cards, whose answer is a short term the
// student picks or types. The quote proves the fact, but not that the model
// took the right term from it: «Как называется период жизни клетки от одного
// деления до следующего, состоящий из интерфазы и собственно деления?» →
// «Интерфаза» came with a verbatim quote and a wrong answer. Two cheap checks
// on word stems (the ones dedup uses) catch such cards:
//
//   - every content word of the answer occurs in the quote, so the answer is
//     taken from the notes, not made up; this also drops an answer like
//     "input", the kind's name written in its place;
//   - the answer does not occur in the question: such a card either gives
//     itself away or, as above, names a term from its own wording.
//
// Boolean and flip answers are sentences that rephrase the quote, so they are
// left to the quote check.

func answerChecked(k Kind) bool {
	return k == KindChoice || k == KindInput
}

// answerSupported reports whether a choice or input answer passes both
// checks against its quote and question.
func answerSupported(c modelCard, quote string) bool {
	return stemsWithin(c.Answer, quote) && !stemsWithin(c.Answer, c.Question)
}

// stemsWithin reports whether every content stem of s occurs in text. An s
// with no content words ("это", "не") is never within.
func stemsWithin(s, text string) bool {
	want := stemSet(questionWords(s))
	if len(want) == 0 {
		return false
	}
	have := stemSet(questionWords(text))
	for stem := range want {
		if !have[stem] {
			return false
		}
	}
	return true
}
