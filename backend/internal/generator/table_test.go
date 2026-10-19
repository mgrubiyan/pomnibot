package generator

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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

// mitosisTable is a Word table as ingest writes it: «cell | cell» per row.
const mitosisTable = "Признак | Митоз | Мейоз\nЧисло делений | одно | два\nДочерние клетки | две | четыре\nНабор хромосом | диплоидный | гаплоидный\nСинтез ДНК | есть | есть"

// organelles is a slide: a heading per group and its terms as bullets.
const organelles = "Органоиды клетки\nДвумембранные:\n• ядро\n• митохондрии\n• пластиды\nОдномембранные:\n• лизосомы\n• вакуоли\nНемембранные:\n• рибосомы"

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
			name:  "groups named together first",
			quote: "Части речи делятся на самостоятельные и служебные. Самостоятельные части речи: существительное, прилагательное, глагол, наречие. Служебные части речи: предлог, союз, частица",
			table: table([]string{"Самостоятельные части речи", "Служебные части речи"}, "существительное", "Самостоятельные части речи", "наречие", "Самостоятельные части речи", "предлог", "Служебные части речи", "частица", "Служебные части речи"),
			want:  true,
		},
		{
			name:  "a bulleted list under each group",
			quote: organelles,
			table: table([]string{"Двумембранные", "Одномембранные"}, "ядро", "Двумембранные", "митохондрии", "Двумембранные", "лизосомы", "Одномембранные", "вакуоли", "Одномембранные"),
			want:  true,
		},
		{
			name:  "a numbered list under each group",
			quote: "Двумембранные:\n1. ядро\n2. митохондрии\nОдномембранные:\n1) лизосомы\n2) вакуоли",
			table: table([]string{"Двумембранные", "Одномембранные"}, "ядро", "Двумембранные", "митохондрии", "Двумембранные", "лизосомы", "Одномембранные", "вакуоли", "Одномембранные"),
			want:  true,
		},
		{
			// рибосомы stand under a heading that is no column of the table.
			name:  "a term under a group left out",
			quote: organelles,
			table: table([]string{"Двумембранные", "Одномембранные"}, "ядро", "Двумембранные", "рибосомы", "Двумембранные", "лизосомы", "Одномембранные", "вакуоли", "Одномембранные"),
			want:  false,
		},
		{
			// Without a marker a line is no list item: «Немембранные» may be
			// a heading, and what follows it is not under «Одномембранные».
			name:  "unmarked lines",
			quote: "Одномембранные:\nлизосомы\nНемембранные\nрибосомы",
			table: table([]string{"Двумембранные", "Одномембранные"}, "лизосомы", "Одномембранные", "рибосомы", "Одномембранные"),
			want:  false,
		},
		{
			name:  "a list under a line naming both groups",
			quote: "Органоиды бывают двумембранные и одномембранные:\n• ядро\n• лизосомы",
			table: table([]string{"Двумембранные", "Одномембранные"}, "ядро", "Двумембранные", "лизосомы", "Одномембранные"),
			want:  false,
		},
		{
			name:  "a comparison table of a document",
			quote: mitosisTable,
			table: table([]string{"Митоз", "Мейоз"}, "Число делений: одно", "Митоз", "Число делений: два", "Мейоз", "Дочерние клетки: четыре", "Мейоз", "Набор хромосом: гаплоидный", "Мейоз"),
			want:  true,
		},
		{
			name:  "a cell under the wrong header",
			quote: mitosisTable,
			table: table([]string{"Митоз", "Мейоз"}, "Число делений: одно", "Мейоз", "Число делений: два", "Мейоз", "Дочерние клетки: две", "Митоз", "Дочерние клетки: четыре", "Мейоз"),
			want:  false,
		},
		{
			// «есть» under both headers sorts nowhere.
			name:  "a value the row repeats",
			quote: mitosisTable,
			table: table([]string{"Митоз", "Мейоз"}, "Число делений: одно", "Митоз", "Число делений: два", "Мейоз", "Синтез ДНК: есть", "Митоз", "Дочерние клетки: четыре", "Мейоз"),
			want:  false,
		},
		{
			name:  "a bare value of a comparison table",
			quote: mitosisTable,
			table: table([]string{"Митоз", "Мейоз"}, "одно", "Митоз", "два", "Мейоз", "две", "Митоз", "четыре", "Мейоз"),
			want:  true,
		},
		{
			// «у» is a stop word: up to content words both cells read «Г, Ц».
			name:  "cells that differ in a stop word",
			quote: "Признак | ДНК | РНК\nУглевод | дезоксирибоза | рибоза\nАзотистые основания | А, Т, Г, Ц | А, У, Г, Ц\nЧисло цепей | две | одна",
			table: table([]string{"ДНК", "РНК"}, "дезоксирибоза", "ДНК", "А, Т, Г, Ц", "ДНК", "А, У, Г, Ц", "РНК", "одна", "РНК"),
			want:  true,
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

// A bare value of a comparison table means nothing on a chip: «два» of
// what? It gets the label of its row, unless that makes it too long.
func TestLabelPipeItems(t *testing.T) {
	quote := mitosisTable + "\nНабор хромосом в дочерних клетках после деления | 2n | n"
	got := labelPipeItems(table([]string{"Митоз", "Мейоз"}, "два", "Мейоз", "Дочерние клетки: две", "Митоз", "n", "Мейоз", "диплоидный", "Митоз"), quote)
	want := []string{"Число делений: два", "Дочерние клетки: две", "n", "Набор хромосом: диплоидный"}
	for i, item := range got.Items {
		if item.Text != want[i] {
			t.Errorf("item %d = %q, want %q", i, item.Text, want[i])
		}
	}
	plain := table(cellColumns, "бактерии", "Прокариоты")
	if labelPipeItems(plain, "К прокариотам относятся бактерии.") != plain {
		t.Errorf("a table of prose changed")
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
		"item names its column": func(m map[string]any) {
			m["columns"] = []string{"Эпителиальная", "Мышечная и нервная"}
			m["items"] = []map[string]string{{"text": "эпителиальная", "column": "Эпителиальная"}, {"text": "кожа", "column": "Эпителиальная"}, {"text": "нервная", "column": "Мышечная и нервная"}, {"text": "мышечная", "column": "Мышечная и нервная"}}
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
	var tables []string
	for i := range maxTablesInChunk + 1 {
		tables = append(tables, fmt.Sprintf(`{"quote":"Q%d","topic":"T","name":"N","question":"Распределите %d","explanation":"E","columns":["A1","B1"],
		 "items":[{"text":"a","column":"A1"},{"text":"b","column":"A1"},{"text":"c","column":"B1"},{"text":"d","column":"B1"}]}`, i, i))
	}
	answer := `{"facts":[` + validFact + `],"tables":[` + strings.Join(tables, ",") + `]}`
	facts, invalid, err := parseAnswer([]byte(answer), 3)
	if err != nil || len(facts) != 1+maxTablesInChunk {
		t.Fatalf("got %d facts, err %v; want the fact and %d tables", len(facts), err, maxTablesInChunk)
	}
	if facts[1].Cards[0].Kind != cards.KindTable || invalid != 1 {
		t.Errorf("table fact = %+v, %d invalid; want a table and the last one over the limit", facts[1], invalid)
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

func TestClassification(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"groups with относятся", "Простые вещества делят на металлы и неметаллы. К металлам относятся железо, медь и натрий. К неметаллам относятся кислород, сера и азот.", true},
		{"terms after the group", "Бактерии и археи относятся к прокариотам, а растения и грибы относятся к эукариотам.", true},
		{"a list per group", "Части речи делятся на самостоятельные и служебные.\nСамостоятельные: существительное, прилагательное, глагол.\nСлужебные: предлог, союз, частица.", true},
		{"lists inside sentences", "Двумембранные органоиды: ядро, митохондрии, пластиды. Одномембранные органоиды: ЭПС, аппарат Гольджи, лизосомы.", true},
		{"bulleted groups", organelles, true},
		{"a comparison table", mitosisTable, true},
		{"a vocabulary table", "cat | кошка\ndog | собака\nhouse | дом", false},
		{"one bulleted list", "Функции белков:\n• строительная\n• ферментативная\n• транспортная", false},
		{"one list is no classification", "Ткани животных бывают четырёх типов: эпителиальная, соединительная, мышечная и нервная.", false},
		{"prose", "Митоз — непрямое деление клетки. Он состоит из четырёх фаз и длится около часа. В анафазе хроматиды расходятся к полюсам.", false},
		{"a vocabulary list", "cat – кошка\ndog – собака\nhouse – дом", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classification(tc.text); got != tc.want {
				t.Errorf("classification = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUserPromptAsksForTable(t *testing.T) {
	c := chunk{Text: "К металлам относятся железо, медь и натрий. К неметаллам относятся кислород, сера и азот."}
	if p := buildUserPrompt("", c, 1, false); !strings.Contains(p, tableNote) {
		t.Errorf("prompt for a classification lacks the table note:\n%s", p)
	}
	c.Text = "Митоз — непрямое деление клетки. Он состоит из четырёх фаз."
	if p := buildUserPrompt("", c, 1, false); strings.Contains(p, tableNote) {
		t.Errorf("prompt for prose asks for a table:\n%s", p)
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
