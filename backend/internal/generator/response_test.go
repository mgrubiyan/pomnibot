package generator

import (
	"context"
	"encoding/json"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
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

func TestGenerationGroupsTypesGroundsEveryCardAndKeepsOrder(t *testing.T) {
	payload := `{"response":{"facts":[{"name":"F","cards":[{"kind":"input","question":"Recall","answer":"A and B"},{"kind":"flip","question":"Easy","answer":"A"},{"kind":"choice","question":"Choose","rightAnswers":["A","B"],"wrongAnswers":["C"]},{"kind":"flip","question":"Harder","answer":"A and B"},{"kind":"boolean","question":"Ungrounded","answer":false}]}]},"grounding":[{"factIndex":0,"question":"Easy","topic":"T","evidence":[{"blockId":"text","quote":"A and B"}],"kind":"flip"},{"factIndex":0,"question":"Harder","topic":"T","evidence":[{"blockId":"text","quote":"A and B"}],"kind":"flip"},{"factIndex":0,"question":"Choose","topic":"T","evidence":[{"blockId":"text","quote":"A and B"}],"kind":"choice"},{"factIndex":0,"question":"Recall","topic":"T","evidence":[{"blockId":"text","quote":"A and B"}],"kind":"input"},{"factIndex":0,"question":"Ungrounded","topic":"T","evidence":[{"blockId":"text","quote":"invented"}],"kind":"boolean"}]}`
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		if !json.Valid(r.Schema) {
			t.Fatal("invalid schema")
		}
		return payload, nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{ID: "d", Text: "A and B"})
	if err != nil || calls != 1 || len(r.Facts) != 1 || len(r.Cards) != 4 || r.Stats.DroppedQuote != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Facts[0].Topic != "T" {
		t.Fatal("lost topic")
	}
	for i, want := range []string{"Easy", "Harder", "Choose", "Recall"} {
		if r.Cards[i].Question != want || r.Cards[i].FactID != r.Facts[0].ID || len(r.Cards[i].Evidence) != 1 {
			t.Fatalf("lost order or provenance: %+v", r.Cards)
		}
	}
	if len(r.Cards[2].RightAnswers) != 2 {
		t.Fatal("lost correct answers")
	}
}

func TestGenerationDropsFactWhenOnlyFlipFailsGrounding(t *testing.T) {
	payload := `{"response":{"facts":[{"name":"F","cards":[{"kind":"flip","question":"Easy","answer":"A"},{"kind":"input","question":"Recall","answer":"A"}]}]},"grounding":[{"factIndex":0,"question":"Easy","topic":"T","evidence":[{"blockId":"text","quote":"invented"}],"kind":"flip"},{"factIndex":0,"question":"Recall","topic":"T","evidence":[{"blockId":"text","quote":"A"}],"kind":"input"}]}`
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return payload, nil }}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "A"})
	if err != nil || len(r.Facts) != 0 || len(r.Cards) != 0 {
		t.Fatalf("fact without valid flip: %+v %v", r, err)
	}
}

func TestSameQuestionAcrossKindsIsRetained(t *testing.T) {
	payload := `{"response":{"facts":[{"name":"F","cards":[{"kind":"flip","question":"Q","answer":"A"},{"kind":"input","question":"Q","answer":"A"}]}]},"grounding":[{"factIndex":0,"kind":"flip","question":"Q","topic":"T","evidence":[{"blockId":"text","quote":"A"}]},{"factIndex":0,"kind":"input","question":"Q","topic":"T","evidence":[{"blockId":"text","quote":"A"}]}]}`
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return payload, nil }}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "A"})
	if err != nil || len(r.Facts) != 1 || len(r.Cards) != 2 {
		t.Fatalf("different kinds treated as duplicate: %+v %v", r, err)
	}
}
