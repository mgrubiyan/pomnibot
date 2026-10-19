package generator

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func table(columns []string, items ...string) *cards.Table {
	t := &cards.Table{Columns: columns}
	for i := 0; i+1 < len(items); i += 2 {
		t.Items = append(t.Items, cards.TableItem{Text: items[i], Column: items[i+1]})
	}
	return t
}

var cellColumns = []string{"Прокариоты", "Эукариоты"}

func TestTableGrounded(t *testing.T) {
	for _, tc := range []struct {
		name, quote string
		table       *cards.Table
		want        bool
	}{
		{
			name:  "groups named before their terms",
			quote: "К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы.",
			table: table(cellColumns, "бактерии", "Прокариоты", "археи", "Прокариоты", "растения", "Эукариоты", "грибы", "Эукариоты"),
			want:  true,
		},
		{
			name:  "groups named after their terms",
			quote: "Бактерии, археи и цианобактерии — прокариоты, растения и грибы — эукариоты.",
			table: table(cellColumns, "бактерии", "Прокариоты", "цианобактерии", "Прокариоты", "растения", "Эукариоты", "грибы", "Эукариоты"),
			want:  true,
		},
		{
			name:  "a line per group",
			quote: "Прокариоты: бактерии, археи\nЭукариоты: растения, грибы, животные",
			table: table(cellColumns, "археи", "Прокариоты", "бактерии", "Прокариоты", "животные", "Эукариоты", "грибы", "Эукариоты"),
			want:  true,
		},
		{
			name:  "a term in the wrong column",
			quote: "К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы.",
			table: table(cellColumns, "бактерии", "Прокариоты", "грибы", "Прокариоты", "растения", "Эукариоты", "животные", "Эукариоты"),
			want:  false,
		},
		{
			name:  "a term not in the quote",
			quote: "К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы.",
			table: table(cellColumns, "бактерии", "Прокариоты", "археи", "Прокариоты", "растения", "Эукариоты", "простейшие", "Эукариоты"),
			want:  false,
		},
		{
			name:  "a column not in the quote",
			quote: "Бактерии и археи не имеют ядра. Растения и грибы имеют ядро.",
			table: table(cellColumns, "бактерии", "Прокариоты", "археи", "Прокариоты", "растения", "Эукариоты", "грибы", "Эукариоты"),
			want:  false,
		},
		{
			name:  "both groups in one clause",
			quote: "Бактерии и грибы бывают прокариотами и эукариотами.",
			table: table(cellColumns, "бактерии", "Прокариоты", "грибы", "Эукариоты", "бактерии", "Прокариоты", "грибы", "Эукариоты"),
			want:  false,
		},
		{
			name:  "a negated group",
			quote: "К прокариотам относятся бактерии и археи. Вирусы не относятся ни к прокариотам, ни к эукариотам. К эукариотам относятся грибы и растения.",
			table: table(cellColumns, "бактерии", "Прокариоты", "вирусы", "Прокариоты", "растения", "Эукариоты", "грибы", "Эукариоты"),
			want:  false,
		},
		{
			name:  "a term set against the group",
			quote: "Бактерии, в отличие от грибов, — прокариоты, а растения — эукариоты.",
			table: table(cellColumns, "бактерии", "Прокариоты", "грибы", "Прокариоты", "растения", "Эукариоты"),
			want:  false,
		},
		{
			// A table read from a PDF row by row: a header, then values.
			// Which value is under which header is lost.
			name:  "a header row",
			quote: "Митоз Мейоз\nдве клетки четыре клетки\nдиплоидные гаплоидные",
			table: table([]string{"Митоз", "Мейоз"}, "две клетки", "Митоз", "четыре клетки", "Мейоз", "диплоидные", "Митоз", "гаплоидные", "Мейоз"),
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tableGrounded(tc.table, tc.quote); got != tc.want {
				t.Errorf("tableGrounded = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseTable(t *testing.T) {
	valid := map[string]any{
		"quote": "Q", "topic": "Строение клетки", "name": "Прокариоты и эукариоты",
		"question": "Распределите организмы по строению клетки", "explanation": "E",
		"columns": []string{"Прокариоты", "Эукариоты"},
		"items": []map[string]string{
			{"text": "бактерии", "column": "Прокариоты"}, {"text": "археи", "column": "прокариоты "},
			{"text": "растения", "column": "Эукариоты"}, {"text": "грибы", "column": "Эукариоты"},
		},
	}
	raw := func(patch func(map[string]any)) json.RawMessage {
		m := map[string]any{}
		for k, v := range valid {
			m[k] = v
		}
		if patch != nil {
			patch(m)
		}
		b, _ := json.Marshal(m)
		return b
	}

	f, ok := parseTable(raw(nil))
	if !ok || len(f.Cards) != 1 {
		t.Fatalf("parseTable = %+v, %v", f, ok)
	}
	c := f.Cards[0]
	if c.Kind != cards.KindTable || c.Question != "Распределите организмы по строению клетки" || f.Topic != "Строение клетки" {
		t.Errorf("card = %+v", c)
	}
	// The feed grades a term by the column string: a column written in
	// another case or with spaces is the column itself.
	if got := c.Table.Items[1].Column; got != "Прокариоты" {
		t.Errorf("column = %q, want the column as listed", got)
	}

	for name, patch := range map[string]func(map[string]any){
		"one column":      func(m map[string]any) { m["columns"] = []string{"Прокариоты"} },
		"four columns":    func(m map[string]any) { m["columns"] = []string{"A", "B", "C", "D"} },
		"repeated column": func(m map[string]any) { m["columns"] = []string{"Прокариоты", "прокариоты"} },
		"unknown column": func(m map[string]any) {
			m["items"] = append(slices.Clone(valid["items"].([]map[string]string)), map[string]string{"text": "вирусы", "column": "Неклеточные"})
		},
		"too few items": func(m map[string]any) { m["items"] = valid["items"].([]map[string]string)[:3] },
		"an empty column": func(m map[string]any) {
			m["items"] = []map[string]string{{"text": "бактерии", "column": "Прокариоты"}, {"text": "археи", "column": "Прокариоты"}, {"text": "цианобактерии", "column": "Прокариоты"}, {"text": "микоплазмы", "column": "Прокариоты"}}
		},
		"repeated item": func(m map[string]any) {
			m["items"] = append(slices.Clone(valid["items"].([]map[string]string)), map[string]string{"text": "Грибы", "column": "Прокариоты"})
		},
		"long item": func(m map[string]any) {
			m["items"] = append(slices.Clone(valid["items"].([]map[string]string)), map[string]string{"text": "организмы с ядром и мембранными органоидами", "column": "Эукариоты"})
		},
		"no quote":          func(m map[string]any) { delete(m, "quote") },
		"no question":       func(m map[string]any) { m["question"] = " " },
		"items not objects": func(m map[string]any) { m["items"] = []string{"бактерии", "археи"} },
	} {
		if _, ok := parseTable(raw(patch)); ok {
			t.Errorf("%s: table accepted", name)
		}
	}
}

func TestParseAnswerReadsTables(t *testing.T) {
	answer := `{"facts":[` + validFact + `],"tables":[
		{"quote":"Q","topic":"T","name":"N","question":"Распределите","explanation":"E","columns":["A1","B1"],
		 "items":[{"text":"a","column":"A1"},{"text":"b","column":"A1"},{"text":"c","column":"B1"},{"text":"d","column":"B1"}]},
		{"quote":"Q2","topic":"T","name":"N","question":"Ещё","explanation":"E","columns":["A1","B1"],
		 "items":[{"text":"a","column":"A1"},{"text":"b","column":"A1"},{"text":"c","column":"B1"},{"text":"d","column":"B1"}]}]}`
	facts, invalid, err := parseAnswer([]byte(answer), 3)
	if err != nil || len(facts) != 2 {
		t.Fatalf("got %d facts, err %v; want the fact and one table", len(facts), err)
	}
	if facts[1].Cards[0].Kind != cards.KindTable || invalid != 1 {
		t.Errorf("table fact = %+v, %d invalid; want a table and the second one over the limit", facts[1], invalid)
	}

	if facts, _, err := parseAnswer([]byte(`{"facts":[],"tables":[]}`), 3); err != nil || len(facts) != 0 {
		t.Errorf("empty answer: %d facts, err %v", len(facts), err)
	}
}

// A table the quote lays out so becomes a card with its terms mixed; one
// with a term in the wrong column goes.
func TestGenerateMakesTableCards(t *testing.T) {
	text := "Все живые организмы делят по строению клетки. К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы. У прокариот нет ядра."
	tableJSON := func(fungi string) string {
		return `{"quote":"К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы.",
			"topic":"Строение клетки","name":"Прокариоты и эукариоты","question":"Распределите организмы по строению клетки",
			"explanation":"Ядро есть только у эукариот.","columns":["Прокариоты","Эукариоты"],
			"items":[{"text":"бактерии","column":"Прокариоты"},{"text":"археи","column":"Прокариоты"},
			         {"text":"растения","column":"Эукариоты"},{"text":"грибы","column":"` + fungi + `"}]}`
	}
	for _, tc := range []struct {
		name, fungi string
		want        bool
	}{{"grounded", "Эукариоты", true}, {"wrong key", "Прокариоты", false}} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
				return `{"facts":[],"tables":[` + tableJSON(tc.fungi) + `]}`, nil
			}}
			res := generate(t, p, Document{Title: "Клетка", Text: text}, Options{})
			if !tc.want {
				if len(res.Cards) != 0 || res.Stats.DroppedTable != 1 {
					t.Errorf("cards = %+v, DroppedTable = %d; want the table dropped", res.Cards, res.Stats.DroppedTable)
				}
				return
			}
			if len(res.Cards) != 1 || res.Cards[0].Kind != cards.KindTable || res.Cards[0].Table == nil {
				t.Fatalf("cards = %+v, want one table card", res.Cards)
			}
			c := res.Cards[0]
			if len(c.Table.Items) != 4 || grouped(c.Table) || c.Answer != "" || c.SourceQuote == "" {
				t.Errorf("card = %+v, table %+v", c, c.Table)
			}
		})
	}
}

func TestShuffleTable(t *testing.T) {
	tb := table([]string{"A1", "B1"}, "a", "A1", "b", "A1", "c", "A1", "d", "B1", "e", "B1", "f", "B1")
	got := shuffleTable(tb, "fact-1")
	if grouped(got) {
		t.Errorf("items = %v, still grouped by column", got.Items)
	}
	if again := shuffleTable(tb, "fact-1"); !slices.Equal(again.Items, got.Items) {
		t.Errorf("shuffle differs between runs: %v and %v", got.Items, again.Items)
	}
	if !slices.Equal(tb.Items, table([]string{"A1", "B1"}, "a", "A1", "b", "A1", "c", "A1", "d", "B1", "e", "B1", "f", "B1").Items) {
		t.Errorf("shuffle changed its argument")
	}
}
