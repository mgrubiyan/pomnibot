package generator

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// "клет-\nка" → "клетка". A soft hyphen counts too: some PDF extractors
	// keep it at the break. Real compounds split at their hyphen ("северо-\nзапад")
	// get glued as well; telling them apart needs a dictionary.
	reHyphenBreak = regexp.MustCompile(`(\p{L})[-\x{00AD}\x{2010}\x{2011}][ \t]*\n[ \t]*(\p{Ll})`)
	reManySpaces  = regexp.MustCompile(`[ ]{2,}`)
	reManyBreaks  = regexp.MustCompile(`\n{3,}`)
	// "а) пункт" starts with a lowercase letter but is a list item, not a
	// continuation of the previous line.
	reLetterItem = regexp.MustCompile(`^\p{Ll}\)`)
)

// normalizeText cleans up text extracted from PDF or pasted by hand: line
// endings, hyphenated words, soft-wrapped lines, exotic and repeated spaces.
// Chunk offsets and quotes refer to the text it returns.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reHyphenBreak.ReplaceAllString(s, "${1}${2}")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune('\n')
		case r == '\f' || r == '\v' || r == '\u2028' || r == '\u2029':
			b.WriteRune('\n')
		case isInvisible(r):
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}

	lines := strings.Split(b.String(), "\n")
	for i, line := range lines {
		lines[i] = reManySpaces.ReplaceAllString(strings.TrimSpace(line), " ")
	}

	// PDF breaks lines inside a paragraph. Lines are joined only when that is
	// clearly the case; headings, list items and table-of-contents entries
	// stay on their own lines, the junk filter needs them there.
	var out strings.Builder
	out.Grow(b.Len())
	for i, line := range lines {
		if i > 0 {
			if line != "" && lines[i-1] != "" && continuesLine(lines[i-1], line) {
				out.WriteByte(' ')
			} else {
				out.WriteByte('\n')
			}
		}
		out.WriteString(line)
	}

	text := reManyBreaks.ReplaceAllString(out.String(), "\n\n")
	return strings.TrimSpace(text)
}

// minWrappedLine is the length from which a line cut off mid-sentence is a
// wrapped paragraph line rather than a heading.
const minWrappedLine = 60

// continuesLine reports whether line continues prev: it starts with a
// lowercase letter ("и\nпостсинтетический"), or prev is a long line cut off
// after a lowercase letter or a comma and line is not a list item
// ("белки и\nРНК").
func continuesLine(prev, line string) bool {
	first, _ := utf8.DecodeRuneInString(line)
	if unicode.IsLower(first) {
		return !reLetterItem.MatchString(line)
	}
	last, _ := utf8.DecodeLastRuneInString(prev)
	return utf8.RuneCountInString(prev) >= minWrappedLine &&
		(unicode.IsLower(last) || last == ',') &&
		unicode.IsLetter(first)
}

// isInvisible reports characters that carry no text: soft hyphen, zero-width
// spaces and joiners, BOM.
func isInvisible(r rune) bool {
	switch r {
	case '\u00AD', '\u200B', '\u200C', '\u200D', '\u2060', '\uFEFF':
		return true
	}
	return false
}

func isQuoteMark(r rune) bool {
	switch r {
	case '"', '\'', '`', '«', '»', '„', '“', '”', '‟', '‘', '’', '‚', '‛', '‹', '›':
		return true
	}
	return false
}

func isDash(r rune) bool {
	switch r {
	case '-', '‐', '‑', '‒', '–', '—', '―', '−', '⁃':
		return true
	}
	return false
}

// folded is a string reduced for fuzzy-but-verbatim comparison, with a map
// from each of its bytes back to the source rune it came from.
type folded struct {
	s     string
	start []int // source offset of the rune that produced s[i]
	end   []int // source offset right after that rune
}

// fold reduces s to a form in which the model's copy of a quote matches the
// notes: lowercase, ё → е, no quote marks, one kind of dash with no spaces
// around it, "…" as three dots, runs of whitespace as one space.
//
// It ignores only typography. Words, their order and punctuation other than
// quotes and dashes must match, so a paraphrase does not pass.
func fold(s string) folded {
	f := folded{
		start: make([]int, 0, len(s)),
		end:   make([]int, 0, len(s)),
	}
	var b strings.Builder
	b.Grow(len(s))

	emit := func(r rune, from, to int) {
		n, _ := b.WriteRune(r)
		for range n {
			f.start = append(f.start, from)
			f.end = append(f.end, to)
		}
	}

	spaceFrom, spaceTo := -1, -1 // pending whitespace run, written lazily
	afterDash := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		from, to := i, i+size
		i = to

		switch {
		case isInvisible(r) || isQuoteMark(r):
			continue
		case unicode.IsSpace(r):
			if spaceFrom < 0 {
				spaceFrom = from
			}
			spaceTo = to
			continue
		case isDash(r):
			spaceFrom = -1
			emit('-', from, to)
			afterDash = true
			continue
		}

		if spaceFrom >= 0 && !afterDash && b.Len() > 0 {
			emit(' ', spaceFrom, spaceTo)
		}
		spaceFrom, afterDash = -1, false

		switch r {
		case '…':
			emit('.', from, to)
			emit('.', from, to)
			emit('.', from, to)
		case 'ё', 'Ё':
			emit('е', from, to)
		default:
			emit(unicode.ToLower(r), from, to)
		}
	}
	f.s = b.String()
	return f
}

// trimQuote drops sentence punctuation and spaces around a folded quote: the
// model often adds a final period or cuts one off. Brackets stay: "(G2)"
// must not lose its closing half, and neither does the minus of "-5".
func trimQuote(s string) string {
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(".,;:!?-", r)
	})
	for {
		r, size := utf8.DecodeRuneInString(s)
		next, _ := utf8.DecodeRuneInString(s[size:])
		if !unicode.IsSpace(r) && !strings.ContainsRune(".,;:!?-", r) || r == '-' && unicode.IsDigit(next) {
			return s
		}
		s = s[size:]
	}
}

// minQuoteRunes rejects quotes too short to prove anything: "клетка" is in
// every fragment of a biology lecture.
const minQuoteRunes = 20

// findQuote looks for the model's quote in the fragment text, ignoring case,
// ё, quote marks, dashes and whitespace. It returns the matching span of the
// fragment itself, so the card shows the notes and not the model's copy.
//
// The match must start and end on word boundaries: "верно, что…" inside
// "неверно, что…" or "митозом" inside "амитозом" would pass as verbatim
// while saying the opposite.
func findQuote(fragment, quote string) (string, bool) {
	q := trimQuote(fold(quote).s)
	if utf8.RuneCountInString(q) < minQuoteRunes {
		return "", false
	}
	f := fold(fragment)
	i := indexPhrase(f.s, q)
	if i < 0 {
		return "", false
	}
	span := fragment[f.start[i]:f.end[i+len(q)-1]]
	return strings.TrimSpace(span), true
}

// indexPhrase is strings.Index that only accepts a match not glued to the
// letters or digits around it: "ион" is not in "функционирование". An edge
// of the phrase that is itself punctuation needs no boundary.
func indexPhrase(s, phrase string) int {
	if phrase == "" {
		return -1
	}
	first, _ := utf8.DecodeRuneInString(phrase)
	last, _ := utf8.DecodeLastRuneInString(phrase)
	for from := 0; from <= len(s)-len(phrase); {
		i := strings.Index(s[from:], phrase)
		if i < 0 {
			return -1
		}
		start, end := from+i, from+i+len(phrase)
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[end:])
		gluedBefore := isWordRune(first) && isWordRune(before)
		gluedAfter := isWordRune(last) && isWordRune(after)
		if !gluedBefore && !gluedAfter {
			return start
		}
		_, size := utf8.DecodeRuneInString(s[start:])
		from = start + size
	}
	return -1
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
