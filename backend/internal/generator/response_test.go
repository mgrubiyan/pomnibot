package generator

import (
	"encoding/json"
	"testing"
)

func TestAIResponseDecodesAllKindsAndPreservesWithinKindOrder(t *testing.T) {
	var r AIResponse
	err := json.Unmarshal([]byte(`{"facts":[{"name":"Fact","cards":[{"kind":"input","question":"hard","answer":"A"},{"kind":"choice","question":"choose","rightAnswers":["A","B"],"wrongAnswers":["C"]},{"kind":"flip","question":"easy","answer":"A"},{"kind":"boolean","question":"true?","answer":false},{"kind":"table","question":"sort","columns":[{"name":"category","rows":["row"]}]},{"kind":"flip","question":"harder","answer":"B"}]}]}`), &r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Facts) != 1 || len(r.Facts[0].Cards) != 6 {
		t.Fatalf("%+v", r)
	}
	cs := r.Facts[0].Cards
	if cs[0].(FlipCard).Question != "easy" || cs[1].(FlipCard).Question != "harder" {
		t.Fatal("unstable difficulty order")
	}
	if len(cs[3].(ChoiceCard).RightAnswers) != 2 {
		t.Fatal("lost right answers")
	}
	if cs[4].(TableCard).Columns[0].Rows[0] != "row" {
		t.Fatal("lost category rows")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip AIResponse
	if err := json.Unmarshal(b, &roundtrip); err != nil {
		t.Fatal(err)
	}
}

func TestAIResponseRejectsMissingFlipAndInvalidKinds(t *testing.T) {
	for _, payload := range []string{
		`{"facts":[{"name":"F","cards":[{"kind":"input","question":"Q","answer":"A"}]}]}`,
		`{"facts":[{"name":"F","cards":[{"kind":"unknown","question":"Q"}]}]}`,
		`{"facts":[{"name":"F","cards":[{"kind":"boolean","question":"Q","answer":"false"}]}]}`,
		`{"facts":[{"name":"F","topic":"metadata","cards":[]}]}`,
	} {
		var r AIResponse
		if json.Unmarshal([]byte(payload), &r) == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}
