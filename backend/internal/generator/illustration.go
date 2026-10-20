package generator

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// Example checks. Notes on programming illustrate a concept with example
// code: class Dog(Animal), obj = C(), a scheme A → B → C. The model takes such
// lines for facts and asks «От какого класса наследуется класс B?» or «Что
// выведет print(obj.hello())?», which a student cannot answer without the code
// in front of them, and should not learn anyway. The prompt asks for the
// concept instead; these checks drop a card that still names what the example
// invented.
//
// Only names an example declares count, never every word of a code line:
// extends, super(), @Override and isinstance are what a lecture teaches. A
// base class (Exception, Animal), a method (__init__, toString) and a field
// may be the subject of the lecture too, so they do not count, except a lone
// letter, which is a placeholder whatever role it plays.

var (
	// "class Dog(Animal)", "interface Shape", "struct Point".
	reTypeDecl = regexp.MustCompile(`\b(?:class|struct|interface|enum|record|trait)\s+([A-Z][A-Za-z0-9_]*)`)
	// "class C(A, B)": its bases, kept if they are lone letters.
	reTypeBases = regexp.MustCompile(`\bclass\s+[A-Za-z_]\w*\s*\(([^)]*)\)`)
	// "Class a", "класс B": a lone letter after the word class.
	reClassLetter = regexp.MustCompile(`(?i:\bclass|(?:^|[^\p{L}])класс\p{L}*)\s+([A-Za-z])\b`)
	// "Car extends Vehicle", "class B : public A".
	reExtends = regexp.MustCompile(`\b([A-Za-z_]\w*)\s+(?:extends|implements)\s+([A-Za-z_]\w*)`)
	reCppBase = regexp.MustCompile(`:\s*(?:public|protected|private)\s+([A-Za-z_]\w*)`)
	// "obj = C()", "A obj = new C();". Formulas assign without a call, and a
	// one-letter name is left out: y = f(x) is maths.
	reInstance = regexp.MustCompile(`\b([A-Za-z_]\w+)\s*=\s*(?:new\s+)?[A-Za-z_][\w.]*\(`)
	// "A → B", "Class a -> Class b".
	reArrow = regexp.MustCompile(`([A-Za-z_]\w*)\s*(?:→|->|⟶)\s*(?:(?i:class|класс)\s+)?([A-Za-z_]\w*)`)
)

// nameSet holds the names an example invented.
type nameSet map[string]struct{}

func (s nameSet) has(name string) bool {
	_, ok := s[name]
	return ok
}

// exampleNames returns the names the examples of text declare, less the
// concepts: a name the title holds or a sentence of the notes defines
// («Singleton — порождающий паттерн») is what the notes are about.
func exampleNames(text, title string) nameSet {
	names := declaredNames(text, true)
	for name := range names {
		if utf8.RuneCountInString(name) > 1 && (wordIn(name, title) || defines(text, name)) {
			delete(names, name)
		}
	}
	return names
}

// declaredNames returns the names text declares. prose adds a lone letter
// after the word «класс» and the ends of arrows: notes declare names so, but
// a card that says «класс B» or «Схема: A → B → C.» does not show the
// example, it only names it.
func declaredNames(text string, prose bool) nameSet {
	names := nameSet{}
	add := func(name string, letterOnly bool) {
		if !letterOnly || utf8.RuneCountInString(name) == 1 {
			names[name] = struct{}{}
		}
	}
	for _, m := range reTypeDecl.FindAllStringSubmatch(text, -1) {
		add(m[1], false)
	}
	for _, m := range reTypeBases.FindAllStringSubmatch(text, -1) {
		for base := range strings.SplitSeq(m[1], ",") {
			add(strings.TrimSpace(base), true)
		}
	}
	for _, m := range reExtends.FindAllStringSubmatch(text, -1) {
		add(m[1], !typeName(m[1]))
		add(m[2], true)
	}
	for _, m := range reCppBase.FindAllStringSubmatch(text, -1) {
		add(m[1], true)
	}
	for _, m := range reInstance.FindAllStringSubmatch(text, -1) {
		add(m[1], false)
	}
	for name := range sqlNames(text) {
		add(name, false)
	}
	if !prose {
		return names
	}
	for _, m := range reClassLetter.FindAllStringSubmatch(text, -1) {
		add(m[1], true)
	}
	// Arrows join reactions, timelines and stacks too (Fe → Fe2O3, Python →
	// Java): only lone letters at both ends make a scheme of classes.
	for i := 0; i < len(text); {
		m := reArrow.FindStringSubmatchIndex(text[i:])
		if m == nil {
			break
		}
		left, right := text[i+m[2]:i+m[3]], text[i+m[4]:i+m[5]]
		if utf8.RuneCountInString(left) == 1 && utf8.RuneCountInString(right) == 1 {
			add(left, true)
			add(right, true)
		}
		i += m[4] // the right end may start the next arrow: A → B → C
	}
	return names
}

// SQL examples. A query or a table definition shows the syntax, and its
// tables, columns and values are invented: CREATE TABLE Student (Id INTEGER,
// …), SELECT * FROM Persons WHERE City='Sandnes'. The model asks «Какой тип у
// поля Id в таблице Student?». Keywords are matched in capitals, as slides
// write them, so that English prose ("data from users") declares nothing.
var (
	// "CREATE TABLE Student", "FROM Persons", "REFERENCES Professor".
	reSQLTable = regexp.MustCompile(`\b(?:TABLE|INTO|FROM|UPDATE|REFERENCES|JOIN|INDEX|ON)\s+([A-Za-z_]\w*)`)
	// The columns of "KEY (ProfId)", "Professor (Id)", "INTO t (a, b)", "ON Person (LastName DESC)".
	reSQLColumnList = regexp.MustCompile(`\b(?:KEY|(?:REFERENCES|INTO|ON)\s+\w+)\s*\(([^()]*)\)`)
	// "WHERE City='Sandnes'", "AND LastName LIKE".
	reSQLCondition = regexp.MustCompile(`\b(?:WHERE|AND|OR|SET)\s+([A-Za-z_]\w*)\s*(?:=|<>|!=|>=|<=|>|<|LIKE\b|BETWEEN\b|IN\b)`)
	// "SELECT Company, OrderNumber FROM".
	reSQLSelect = regexp.MustCompile(`\bSELECT\s+(?:DISTINCT\s+)?([^*\n]+?)\s+FROM\b`)
	// "CREATE TABLE Student (": its column definitions follow.
	reSQLCreate = regexp.MustCompile(`\bCREATE\s+TABLE\s+\w+\s*\(`)
	// 'Los Angeles', ‘freshman’: a value, on a line of SQL only.
	reSQLLiteral   = regexp.MustCompile(`['‘’]([^'‘’\n]+)['‘’]`)
	reSQLStatement = regexp.MustCompile(`\b(?:SELECT|INSERT|UPDATE|DELETE|VALUES|WHERE|DEFAULT|CHECK)\b`)
	reIdentStart   = regexp.MustCompile(`^\s*([A-Za-z_]\w*)`)
	// table_name, column_1, value1, {condition}: placeholders of a syntax
	// template, the lecture's own words.
	reSQLPlaceholder = regexp.MustCompile(`(?i)^(?:(?:table|column|index|field|view)_?name|column_?\d*|value_?\d*|condition|pattern|table|column)$`)
)

// reSQLKeyword is a keyword, a type or an aggregate: what a lecture teaches.
var reSQLKeyword = regexp.MustCompile(`(?i)^(?:SELECT|DISTINCT|FROM|WHERE|AND|OR|NOT|IN|LIKE|BETWEEN|IS|NULL|ORDER|GROUP|BY|HAVING|ASC|DESC|` +
	`INSERT|INTO|VALUES|UPDATE|SET|DELETE|CREATE|ALTER|DROP|TRUNCATE|TABLE|INDEX|VIEW|UNIQUE|PRIMARY|FOREIGN|KEY|` +
	`REFERENCES|CONSTRAINT|CHECK|DEFAULT|ON|CASCADE|RESTRICT|NO|ACTION|ADD|COLUMN|JOIN|INNER|LEFT|RIGHT|FULL|OUTER|` +
	`UNION|ALL|AS|TOP|LIMIT|EXISTS|ANY|SOME|INTEGER|INT|SMALLINT|TINYINT|BIGINT|CHAR|VARCHAR|TEXT|FLOAT|DOUBLE|REAL|` +
	`DECIMAL|NUMERIC|DATE|TIME|DATETIME|TIMESTAMP|BOOLEAN|COUNT|SUM|AVG|MIN|MAX)$`)

// maxTableDefinition bounds the column definitions read after "CREATE TABLE
// t (": a closing parenthesis lost in a PDF must not make the rest of the
// notes columns.
const maxTableDefinition = 2000

// sqlNames returns the tables, columns and values that SQL in text invents.
func sqlNames(text string) nameSet {
	names := nameSet{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || reSQLKeyword.MatchString(name) || reSQLPlaceholder.MatchString(name) {
			return
		}
		names[name] = struct{}{}
	}
	addFirst := func(item string) {
		if m := reIdentStart.FindStringSubmatch(item); m != nil {
			add(m[1])
		}
	}
	for _, m := range reSQLTable.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range reSQLColumnList.FindAllStringSubmatch(text, -1) {
		for item := range strings.SplitSeq(m[1], ",") {
			addFirst(item)
		}
	}
	for _, m := range reSQLCondition.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range reSQLSelect.FindAllStringSubmatch(text, -1) {
		for item := range strings.SplitSeq(m[1], ",") {
			addFirst(item)
		}
	}
	for _, loc := range reSQLCreate.FindAllStringIndex(text, -1) {
		def := text[loc[1]:]
		def = def[:min(len(def), maxTableDefinition)]
		for _, item := range topLevelItems(def) {
			addFirst(item)
		}
	}
	for line := range strings.SplitSeq(text, "\n") {
		if !reSQLStatement.MatchString(line) {
			continue
		}
		for _, m := range reSQLLiteral.FindAllStringSubmatch(line, -1) {
			// 'A' is a grade, 'Jan-10-1999' a date: no name to ask about.
			if v := m[1]; utf8.RuneCountInString(v) > 1 && !strings.ContainsFunc(v, unicode.IsDigit) {
				add(v)
			}
		}
	}
	return names
}

// topLevelItems splits the column definitions that follow "CREATE TABLE t (" at
// its top-level commas, up to the closing parenthesis.
func topLevelItems(s string) []string {
	var items []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return append(items, s[start:i])
			}
			depth--
		case ',':
			if depth == 0 {
				items = append(items, s[start:i])
				start = i + 1
			}
		}
	}
	return append(items, s[start:])
}

// typeName reports a name written as a class: capitalized, letters and
// digits.
func typeName(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r) && !strings.Contains(s, "_")
}

// defines reports whether a sentence or a line of text starts by defining
// name: «Singleton — порождающий паттерн», «Singleton (одиночка) — …».
func defines(text, name string) bool {
	re := regexp.MustCompile(`(?:^|[\n.!?]\s*)` + regexp.QuoteMeta(name) + `(?:\s*\([^)]*\))?\s+[—–-]\s`)
	return re.MatchString(text)
}

// aboutExample reports whether a card asks about an example rather than the
// concept it shows: its question, or a claim it shows in the question's place,
// names an example name, or the short answer the student picks or types is
// one. A name the card declares itself is fine: «Что выведет print(x) после
// x = f(2)?» carries its example.
//
// In a Russian card any Latin name is code. In an English one a lone letter
// may be a word ("A class may extend…"), and so may a lowercase name, so only
// capitalized names of two letters or more count there.
func aboutExample(c modelCard, names nameSet) bool {
	if len(names) == 0 {
		return false
	}
	own := declaredNames(c.Question+"\n"+c.Alt, false)
	russian := strings.IndexFunc(c.Question, isCyrillic) >= 0
	texts := []string{c.Question}
	if c.Kind == cards.KindBoolean {
		texts = append(texts, c.Alt)
	}
	for name := range names {
		if own.has(name) {
			continue
		}
		if !russian && (utf8.RuneCountInString(name) == 1 || !typeName(name)) {
			continue
		}
		for _, t := range texts {
			if wordIn(name, t) {
				return true
			}
		}
		if answerChecked(c.Kind) && wordIn(name, c.Answer) {
			return true
		}
	}
	return false
}

func isCyrillic(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }

// wordIn reports whether name occurs in text as a whole word, case and all:
// not A of "API", not C of "C++" or "C#".
func wordIn(name, text string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !identRune(before) && !identRune(after) && after != '+' && after != '#' {
			return true
		}
		i = end
	}
}

func identRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}
