package generator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStoredFactDecodesAllKindsAndPreservesWithinKindOrder(t *testing.T) {
	var r Result
	err := json.Unmarshal([]byte(`{"figures":[],"issues":[],"facts":[{"name":"Fact","pages":[1],"cards":[{"kind":"input","source":"source","sourceQuote":"","question":"hard","answer":"A"},{"kind":"choice","source":"source","sourceQuote":"","question":"choose","rightAnswers":["A","B"],"wrongAnswers":["C"]},{"kind":"flip","source":"source","sourceQuote":"","question":"easy","answer":"A"},{"kind":"boolean","source":"source","sourceQuote":"","question":"true?","answer":false},{"kind":"table","source":"source","sourceQuote":"","question":"sort","columns":[{"name":"category","rows":["row"]}]},{"kind":"flip","source":"source","sourceQuote":"","question":"harder","answer":"B"}]}]}`), &r)
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

func TestAIResponseDecodesMarkdownTablesInQuestionAndExtraCards(t *testing.T) {
	markdownTableQuestion := "Дана таблица Persons:\n\n| LastName | FirstName | Year |\n| --- | --- | --- |\n| Hansen | Ola | 1951 |\n| Svendson | Tove | 1978 |\n\nКакие строки вернет запрос `WHERE Year > 1965`?"
	payload := `{"figures":[],"issues":[],"facts":[{"name":"Фильтрация по году","pages":[12],"recall":{"question":` +
		jsonString(markdownTableQuestion) + `,"answer":"Строку с Tove Svendson (1978).","source":"WHERE Year > 1965 отбирает записи с годом строго больше 1965.","sourceQuote":""},"extraCards":[{"kind":"choice","question":"Какая запись условия верна для числового столбца Year?","rightAnswers":["WHERE Year > 1965"],"wrongAnswers":["WHERE Year > '1965'"],"source":"Числовые константы пишутся без апострофов.","sourceQuote":""},{"kind":"table","question":"Распределите литералы по типам:","columns":[{"name":"Числовые","rows":["1965","1978"]},{"name":"Строковые","rows":["'Tove'","'Sandnes'"]}],"source":"Числа без кавычек, строки в апострофах.","sourceQuote":""}]}]}`

	var r AIResponse
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		t.Fatalf("failed to decode AIResponse with markdown table and extraCards: %v", err)
	}
	if len(r.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(r.Facts))
	}
	fact := r.Facts[0]
	// recall expands to FlipCard + InputCard, plus 1 ChoiceCard and 1 TableCard = 4 cards total
	if len(fact.Cards) != 4 {
		t.Fatalf("expected 4 expanded cards, got %d", len(fact.Cards))
	}
	var foundTableInQuestion bool
	var foundChoice bool
	var foundTableCard bool
	for _, c := range fact.Cards {
		if strings.Contains(c.Base().Question, "| LastName | FirstName |") {
			foundTableInQuestion = true
		}
		switch card := c.(type) {
		case ChoiceCard:
			foundChoice = true
			if len(card.RightAnswers) != 1 || card.RightAnswers[0] != "WHERE Year > 1965" {
				t.Errorf("unexpected choice rightAnswers: %+v", card.RightAnswers)
			}
		case TableCard:
			foundTableCard = true
			if len(card.Columns) != 2 || card.Columns[0].Name != "Числовые" {
				t.Errorf("unexpected table card columns: %+v", card.Columns)
			}
		}
	}
	if !foundTableInQuestion {
		t.Error("expected markdown table in question to be preserved")
	}
	if !foundChoice {
		t.Error("expected ChoiceCard to be preserved")
	}
	if !foundTableCard {
		t.Error("expected TableCard to be preserved")
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
