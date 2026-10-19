package generator

import (
	"encoding/json"
	"hash/fnv"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// Table cards. A classification of the notes («К прокариотам относятся
// бактерии и археи, к эукариотам — растения и грибы») becomes a sorting card:
// the student lays the terms out across the columns. A wrong key teaches a
// wrong fact, and the model is no more careful about a table than about a
// quote, so the key is checked against the quote in code (tableGrounded): a
// term must stand next to its own column there.
//
// The model returns tables apart from facts, in "tables": a card of its own
// kind with columns and items would not fit the card schema, where every
// field is required.

// Table size. The feed lays columns out two in a row on a phone and terms as
// chips, so both stay few and short.
const (
	minTableColumns  = 2
	maxTableColumns  = 3
	minTableItems    = 4
	maxTableItems    = 8
	maxTableItemLen  = 40 // runes: «Набор хромосом: гаплоидный» fits
	maxTablesInChunk = 3
)

// Field names of a table in the model's answer, besides the fact's.
const (
	fieldColumns = "columns"
	fieldItems   = "items"
	fieldText    = "text"
	fieldColumn  = "column"
)

// parseTable reads {"quote", "topic", "name", "question", "explanation",
// "columns", "items": [{"text", "column"}]} as a fact with one table card.
// A table out of size, with a term in no column or a column with no terms,
// is rejected whole: a table without a term is a different card.
func parseTable(data json.RawMessage) (modelFact, bool) {
	fields, ok := objectFields(data)
	if !ok {
		return modelFact{}, false
	}
	quote, okQuote := stringField(fields, fieldQuote)
	topic, okTopic := stringField(fields, fieldTopic)
	question, okQuestion := stringField(fields, fieldQuestion)
	explanation, _ := stringField(fields, fieldExplanation)
	if !okQuote || !okTopic || !okQuestion {
		return modelFact{}, false
	}

	columns := stringList(fields, fieldColumns)
	if len(columns) < minTableColumns || len(columns) > maxTableColumns {
		return modelFact{}, false
	}
	for i, c := range columns {
		columns[i] = strings.TrimSpace(c)
		if slices.ContainsFunc(columns[:i], func(prev string) bool { return strings.EqualFold(prev, columns[i]) }) {
			return modelFact{}, false
		}
	}

	var items []json.RawMessage
	if raw, ok := fields[fieldItems]; !ok || json.Unmarshal(raw, &items) != nil ||
		len(items) < minTableItems || len(items) > maxTableItems {
		return modelFact{}, false
	}
	t := &cards.Table{Columns: columns}
	filled := map[string]bool{}
	for _, raw := range items {
		item, ok := objectFields(raw)
		if !ok {
			return modelFact{}, false
		}
		text, okText := stringField(item, fieldText)
		column, okColumn := stringField(item, fieldColumn)
		if !okText || !okColumn || utf8.RuneCountInString(text) > maxTableItemLen {
			return modelFact{}, false
		}
		// The feed grades a term by the column string, so it must be one of
		// the columns as listed.
		i := slices.IndexFunc(columns, func(c string) bool { return strings.EqualFold(c, strings.TrimSpace(column)) })
		if i < 0 || slices.ContainsFunc(t.Items, func(prev cards.TableItem) bool { return strings.EqualFold(prev.Text, text) }) {
			return modelFact{}, false
		}
		// A term that a column names, or the other way round, sorts itself:
		// «нервная» under «Мышечная и нервная».
		if slices.ContainsFunc(columns, func(c string) bool { return wordsWithin(text, c) || wordsWithin(c, text) }) {
			return modelFact{}, false
		}
		t.Items = append(t.Items, cards.TableItem{Text: text, Column: columns[i]})
		filled[columns[i]] = true
	}
	if len(filled) != len(columns) {
		return modelFact{}, false
	}

	card := modelCard{Kind: cards.KindTable, Question: question, Explanation: explanation, Table: t}
	return modelFact{Quote: quote, Topic: topic, Name: factName(fields, topic), Cards: []modelCard{card}}, true
}

// tableGrounded reports whether the quote puts every term of t in its
// column. The quote is cut into lines, a line into sentences, a sentence into
// clauses at commas. A term belongs to the one column its clause names;
// a clause naming none continues the list of the nearest clause before it in
// the sentence that names one, or else of the nearest one after it:
//
//	К прокариотам относятся бактерии и археи, к эукариотам — растения, животные и грибы.
//	Бактерии, археи — прокариоты, растения и грибы — эукариоты.
//	Прокариоты: бактерии, археи
//
// A list item («• ядро», «1. ядро») naming no column belongs to the line
// above its list, which must name exactly one: a slide with a heading per
// group. Only a marked line counts as an item: «Немембранные» on a line of
// its own may be a heading of a group the table left out.
//
// A clause that negates or sets a term apart («Вирусы не относятся к
// прокариотам», «в отличие от грибов») puts it in no column. A clause naming
// two columns tells nothing, and neither does a table read from a PDF row by
// row, where a header line names the columns and the values follow on lines
// of their own: such tables are dropped.
func tableGrounded(t *cards.Table, quote string) bool {
	for _, c := range t.Columns {
		if !namedIn(c, quote) {
			return false
		}
	}
	if rows := pipeRows(quote); rows != nil {
		return pipeGrounded(t, rows)
	}
	const ambiguous = "\x00"
	columnIn := func(text string) string {
		found := ""
		for _, col := range t.Columns {
			if namedIn(col, text) {
				if found != "" {
					return ambiguous
				}
				found = col
			}
		}
		return found
	}
	type clause struct {
		text   string
		column string // "" for none, ambiguous for two or more
	}
	type sentence struct {
		clauses []clause
		line    int
	}
	type line struct {
		item   bool // a list item
		column string
	}
	var lines []line
	var sentences []sentence
	for i, raw := range strings.Split(quote, "\n") {
		body, item := listItem(raw)
		lines = append(lines, line{item: item, column: columnIn(body)})
		for _, s := range splitSentences(body) {
			st := sentence{line: i}
			for part := range strings.SplitSeq(s, ",") {
				st.clauses = append(st.clauses, clause{text: part, column: columnIn(part)})
			}
			sentences = append(sentences, st)
		}
	}

	columnOf := func(item string, in func(s, text string) bool) string {
		for _, s := range sentences {
			cl := s.clauses
			for i, c := range cl {
				if !in(item, c.text) {
					continue
				}
				if negated(c.text) {
					return ""
				}
				if c.column != "" {
					return c.column
				}
				for j := i - 1; j >= 0; j-- {
					if cl[j].column != "" {
						return cl[j].column
					}
				}
				for j := i + 1; j < len(cl); j++ {
					if cl[j].column != "" {
						return cl[j].column
					}
				}
				if !lines[s.line].item || lines[s.line].column != "" {
					return ""
				}
				j := s.line - 1
				for j >= 0 && lines[j].item && lines[j].column == "" {
					j--
				}
				if j < 0 {
					return ""
				}
				return lines[j].column
			}
		}
		return ""
	}
	for _, item := range t.Items {
		// A term is looked up word for word first, then up to endings: «частица»
		// is a form of «части», and «части речи» names both columns, but
		// «перетяжка» stands in the notes as «перетяжкой».
		in := namedIn
		if wordsWithin(item.Text, quote) {
			in = wordsWithin
		}
		if col := columnOf(item.Text, in); col == "" || col == ambiguous || col != item.Column {
			return false
		}
	}
	return true
}

// Comparison tables. Ingest writes a row of a Word table as «cell | cell»,
// so a table of the notes keeps its layout: a header row of what is compared
// and rows of «признак | значение | значение». A term is a value, written
// «Число делений: два» or bare, and belongs to the header above its cell.
const (
	minPipeCells = 3 // a label and two values: «Число делений | одно | два»
	minPipeRows  = 3 // the header and two rows
)

// pipeRows returns the cells of the quote's comparison table, header first,
// or nil if the quote holds none.
func pipeRows(quote string) [][]string {
	var rows [][]string
	for line := range strings.SplitSeq(quote, "\n") {
		if !strings.Contains(line, " | ") {
			continue
		}
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) < minPipeCells || len(rows) > 0 && len(cells) != len(rows[0]) {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) < minPipeRows {
		return nil
	}
	return rows
}

// pipeGrounded reports whether every term of t is a value of exactly one
// cell, a value its row does not repeat, under the header of its column.
func pipeGrounded(t *cards.Table, rows [][]string) bool {
	for _, item := range t.Items {
		_, col, ok := pipeCell(rows, item.Text)
		if !ok || !namedIn(item.Column, rows[0][col]) {
			return false
		}
	}
	return true
}

// pipeCell finds the one cell of the table body a term stands for: «два» or
// «Число делений: два». The cell is matched whole first, then up to endings:
// «А, У, Г, Ц» and «А, Т, Г, Ц» differ only in stop words.
func pipeCell(rows [][]string, term string) (row, col int, ok bool) {
	label, value := "", strings.TrimSpace(term)
	if l, v, found := strings.Cut(term, ":"); found {
		label, value = strings.TrimSpace(l), strings.TrimSpace(v)
	}
	for _, match := range []func(s, cell string) bool{
		func(s, cell string) bool { return strings.EqualFold(s, cell) },
		namedIn,
	} {
		found := 0
		for r := 1; r < len(rows); r++ {
			if label != "" && !namedIn(label, rows[r][0]) {
				continue
			}
			for k := 1; k < len(rows[r]); k++ {
				if match(value, rows[r][k]) {
					found++
					row, col = r, k
				}
			}
		}
		switch {
		case found == 1:
			return row, col, true
		case found > 1:
			return 0, 0, false // in two cells: of two rows, or of one row twice
		}
	}
	return 0, 0, false
}

// labelPipeItems returns t with every bare value of the quote's comparison
// table prefixed with the label of its row: «два» means nothing on a chip,
// «Число делений: два» does. A label that makes the term too long is left
// out. A table of prose comes back as it is.
func labelPipeItems(t *cards.Table, quote string) *cards.Table {
	rows := pipeRows(quote)
	if rows == nil {
		return t
	}
	out := &cards.Table{Columns: t.Columns, Items: slices.Clone(t.Items)}
	for i, item := range out.Items {
		if strings.Contains(item.Text, ":") {
			continue
		}
		r, _, ok := pipeCell(rows, item.Text)
		if !ok {
			continue
		}
		if labeled := rows[r][0] + ": " + item.Text; utf8.RuneCountInString(labeled) <= maxTableItemLen {
			out.Items[i].Text = labeled
		}
	}
	return out
}

// listItem returns a line without its list marker (see reListItem) and
// whether it had one.
func listItem(line string) (string, bool) {
	if m := reListItem.FindStringIndex(line); m != nil {
		return line[m[1]:], true
	}
	return line, false
}

// wordsWithin reports whether every content word of s occurs in text as it
// is, ending and all.
func wordsWithin(s, text string) bool {
	want := contentWords(s)
	if len(want) == 0 {
		return false
	}
	have := contentWords(text)
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h []rune) bool { return slices.Equal(w, h) }) {
			return false
		}
	}
	return true
}

// negationWords turn a clause against the term it names.
var negationWords = []string{"не", "нет", "ни", "кроме", "исключением", "отличие", "отличии"}

func negated(clause string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(clause), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if slices.Contains(negationWords, w) {
			return true
		}
	}
	return false
}

var (
	// «К металлам относятся железо и медь», «бактерии относятся к прокариотам».
	reBelongs = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:к\s+\p{L}+\s+относ\p{L}*|относ\p{L}*\s+к\s+\p{L}+)`)
	// «Служебные: предлог, союз, частица» — a group and a list of two commas.
	reGroupList = regexp.MustCompile(`\p{L}:\s*[^.:\n]+,[^.:\n]+,`)
)

// minGroupStatements is how many statements of a fragment must put terms in
// groups for it to hold a classification: one list is not yet a table.
const minGroupStatements = 2

// minGroupItems is how many list items under a heading make a group.
const minGroupItems = 2

// classification reports a fragment that puts terms in groups, so the user
// prompt asks for a table outright. The system prompt asks for one too, but a
// model given a long list of rules tends to leave tables empty.
func classification(text string) bool {
	n := len(reBelongs.FindAllStringIndex(text, -1)) + len(reGroupList.FindAllStringIndex(text, -1))
	// A slide: a heading, then two list items or more under it.
	items, heading := 0, false
	for line := range strings.SplitSeq(text, "\n") {
		if _, item := listItem(line); item {
			items++
			continue
		}
		if heading && items >= minGroupItems {
			n++
		}
		heading, items = strings.TrimSpace(line) != "", 0
	}
	if heading && items >= minGroupItems {
		n++
	}
	if pipeRows(text) != nil {
		n += minGroupStatements
	}
	return n >= minGroupStatements
}

// splitSentences cuts text at line breaks and at the ends of sentences.
func splitSentences(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '.' || r == ';' || r == '!' || r == '?'
	})
}

// shuffleTable returns t with its items in an order that does not give the
// columns away: the model lists them column by column, and the feed shows
// them as they come. The order depends on the fact only, so a regenerated
// set looks the same.
func shuffleTable(t *cards.Table, factID string) *cards.Table {
	h := fnv.New64a()
	_, _ = h.Write([]byte(factID))
	rng := rand.New(rand.NewPCG(h.Sum64(), 0))
	out := &cards.Table{Columns: slices.Clone(t.Columns), Items: slices.Clone(t.Items)}
	for range 8 {
		rng.Shuffle(len(out.Items), func(i, j int) { out.Items[i], out.Items[j] = out.Items[j], out.Items[i] })
		if !grouped(out) {
			break
		}
	}
	return out
}

// grouped reports whether the items of each column come one after another.
func grouped(t *cards.Table) bool {
	seen := map[string]bool{}
	for i, item := range t.Items {
		if i > 0 && item.Column == t.Items[i-1].Column {
			continue
		}
		if seen[item.Column] {
			return false
		}
		seen[item.Column] = true
	}
	return true
}
