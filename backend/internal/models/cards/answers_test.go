package cards

import "testing"

func TestChoiceAnswerMatchesWholeSet(t *testing.T) {
	for _, tc := range []struct {
		given string
		want  bool
	}{
		{`["B","A"]`, true}, {`["A"]`, false}, {`["A","B","C"]`, false}, {`["A","A"]`, false}, {`A`, false},
	} {
		if got := MatchChoiceAnswer(`["A","B"]`, tc.given); got != tc.want {
			t.Fatalf("%s: %v", tc.given, got)
		}
	}
	if !MatchChoiceAnswer("A", `["A"]`) {
		t.Fatal("legacy answer incompatible")
	}
}
