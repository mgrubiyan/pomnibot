package generator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStoredFactDecodesAllKindsAndPreservesWithinKindOrder(t *testing.T) {
	var r Result
	err := json.Unmarshal([]byte(`{"issues":[],"facts":[{"name":"Fact","pages":[1],"cards":[{"kind":"input","source":"source","sourceQuote":"","question":"hard","answer":"A"},{"kind":"choice","source":"source","sourceQuote":"","question":"choose","rightAnswers":["A","B"],"wrongAnswers":["C"]},{"kind":"flip","source":"source","sourceQuote":"","question":"easy","answer":"A"},{"kind":"boolean","source":"source","sourceQuote":"","question":"true?","answer":false},{"kind":"table","source":"source","sourceQuote":"","question":"sort","columns":[{"name":"category","rows":["row"]}]},{"kind":"flip","source":"source","sourceQuote":"","question":"harder","answer":"B"}]}]}`), &r)
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
}

func TestAIResponseRejectsInvalidKindsAndFields(t *testing.T) {
	for _, payload := range []string{
		strings.Replace(directReply(), `"extraCards":[]`, `"extraCards":[{"kind":"unknown"}]`, 1),
		strings.Replace(directReply(), `"extraCards":[]`, `"extraCards":[{"kind":"boolean","answer":"A"}]`, 1),
		strings.Replace(directReply(), `"pages":[1]`, `"pages":[1],"topic":"metadata"`, 1),
	} {
		var r AIResponse
		if json.Unmarshal([]byte(payload), &r) == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}

func TestStoredFactsRoundTripRetainsAllCardSourcesAndKinds(t *testing.T) {
	f := requiredFormatsFixture()
	for i, c := range f.Cards {
		switch v := c.(type) {
		case InputCard:
			v.Source = "input source"
			v.SourceQuote = "input quote"
			f.Cards[i] = v
		case FlipCard:
			v.Source = "flip source"
			v.SourceQuote = "flip quote"
			f.Cards[i] = v
		}
	}
	b, err := json.Marshal(struct {
		Facts  []Fact   `json:"facts"`
		Issues []string `json:"issues"`
	}{[]Fact{f}, []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Facts  []Fact   `json:"facts"`
		Issues []string `json:"issues"`
	}
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Facts[0].Cards[0].(FlipCard).Source != "flip source" || got.Facts[0].Cards[4].(InputCard).SourceQuote != "input quote" {
		t.Fatal("lost card metadata")
	}
	b, err = json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Facts []struct {
			Pages []int
			Cards []map[string]any
		}
		Issues []string
	}
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Facts[0].Pages) != 1 || wire.Issues == nil {
		t.Fatal("lost response fields")
	}
	for _, c := range wire.Facts[0].Cards {
		if c["kind"] == nil || c["source"] == nil || c["sourceQuote"] == nil {
			t.Fatal("missing wire discriminator or source")
		}
	}
}
