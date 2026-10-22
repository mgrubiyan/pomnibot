package generator

import "strings"

// A causal question needs a reason beyond its own premise. Repeating that an
// effect happens, with rhetorical or evaluative words, does not explain why.
// Short causal answers with new substantive content still proceed to review.
func causalRestatement(c modelCard) bool {
	if c.LearningMode == "apply" && hasApplicationContext(c.Question) {
		return false
	}
	parts := strings.Split(strings.TrimSpace(c.Question), "\n\n")
	question := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if !strings.HasPrefix(question, "почему ") && !strings.HasPrefix(question, "зачем ") {
		return false
	}
	q, a := stemSet(questionWords(question)), stemSet(questionWords(c.Answer))
	rhetoric := stemSet(questionWords("потому поскольку поэтому так наличие резко процесс часто редко ухудшает улучшает целесообразно рекомендуется может могут причина является"))
	for word := range a {
		if !q[word] && !rhetoric[word] {
			return false
		}
	}
	return len(a) > 0
}
