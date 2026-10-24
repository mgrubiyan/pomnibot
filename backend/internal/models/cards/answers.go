package cards

import (
	"encoding/json"
	"slices"
	"strings"
)

// ChoiceAnswers reads multiple stored correct answers, including legacy single answers.
func ChoiceAnswers(answer string) []string {
	var values []string
	if json.Unmarshal([]byte(answer), &values) == nil && len(values) > 0 {
		return values
	}
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	return []string{answer}
}

// MatchChoiceAnswer requires exactly the correct set, independent of selection order.
func MatchChoiceAnswer(expected, given string) bool {
	want := ChoiceAnswers(expected)
	got := ChoiceAnswers(given)
	if len(want) == 0 || len(want) != len(got) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] || !slices.Contains(want, v) {
			return false
		}
		seen[v] = true
	}
	return true
}
