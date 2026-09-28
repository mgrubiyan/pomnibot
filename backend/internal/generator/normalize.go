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

// isBullet reports list markers, which slides are full of and the model
// leaves out of its quotes. The middle dot is not one: "кг·м/с²".
func isBullet(r rune) bool {
	switch r {
	case '▶', '►', '▸', '▹', '•', '◦', '▪', '▫', '■', '□', '●', '○', '‣', '∙', '➢', '➤',
		'\uF0B7', '\uF0A7', '\uF076': // Symbol and Wingdings bullets of Word PDFs
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
// around it, "…" as three dots, list bullets and runs of whitespace as one
// space.
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
		case unicode.IsSpace(r) || isBullet(r):
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
//
// The model sometimes glues a quote from sentences that are not adjacent in
// the notes, with "..." or silently. Such a quote is accepted when each of
// its sentences is verbatim, in order and close to the previous one; the
// card then shows the whole passage, what was left out included.
func findQuote(fragment, quote string) (string, bool) {
	start, end, ok := locateQuote(fragment, quote)
	return fragment[start:end], ok
}

// locateQuote is findQuote giving the byte offsets of the span in fragment.
func locateQuote(fragment, quote string) (start, end int, ok bool) {
	q := trimQuote(fold(quote).s)
	if utf8.RuneCountInString(q) < minQuoteRunes {
		return 0, 0, false
	}
	f := fold(fragment)
	if i := indexPhrase(f.s, q, 0); i >= 0 {
		start, end = spanOf(fragment, f, i, i+len(q))
		return start, end, true
	}

	pieces := quotePieces(q)
	if len(pieces) < 2 {
		return 0, 0, false
	}
	first, last := -1, 0
	for _, p := range pieces {
		i := indexPhrase(f.s, p, last)
		if i < 0 {
			return 0, 0, false
		}
		if first < 0 {
			first = i
		} else if utf8.RuneCountInString(f.s[last:i]) > maxQuoteGap {
			return 0, 0, false
		}
		last = i + len(p)
	}
	if utf8.RuneCountInString(f.s[first:last]) > maxQuoteSpan {
		return 0, 0, false
	}
	start, end = spanOf(fragment, f, first, last)
	return start, end, true
}

const (
	// maxQuoteGap is how much of the notes may be left out between two
	// sentences of a quote: a sentence or two, not a page.
	maxQuoteGap = 400
	// maxQuoteSpan bounds the passage a quote in pieces stands for.
	maxQuoteSpan = 800
)

// reQuoteBreak splits a folded quote into sentences: at "..." and at the
// end of a sentence.
var reQuoteBreak = regexp.MustCompile(`\.\.\.|[.!?]\s`)

// quotePieces splits a folded quote into sentences to look up one by one. A
// piece shorter than minQuoteRunes is kept with its neighbour, as written:
// "т. е." does not split a sentence, and "... до её гибели" is too little to
// find on its own.
func quotePieces(q string) []string {
	var pieces []string
	var starts []int
	from := 0 // start of the piece being grown
	for _, m := range append(reQuoteBreak.FindAllStringIndex(q, -1), []int{len(q), len(q)}) {
		piece := trimQuote(q[from:m[0]])
		if utf8.RuneCountInString(piece) < minQuoteRunes {
			continue // grows up to the next break
		}
		pieces, starts = append(pieces, piece), append(starts, from)
		from = m[1]
	}
	if tail := trimQuote(q[from:]); tail != "" {
		if len(pieces) == 0 {
			return nil
		}
		last := len(pieces) - 1
		pieces[last] = trimQuote(q[starts[last]:])
	}
	return pieces
}

// spanOf returns the offsets of the source text behind f.s[i:j], without
// the whitespace around it.
func spanOf(source string, f folded, i, j int) (start, end int) {
	start, end = f.start[i], f.end[j-1]
	s := source[start:end]
	start += len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
	end -= len(s) - len(strings.TrimRightFunc(s, unicode.IsSpace))
	return start, end
}

// normalizedPageStarts maps page starts, byte offsets in the original text,
// to offsets in its normalized form. Normalization only drops whitespace,
// invisible and control characters and the hyphens of words broken over
// lines; every other character stays, in order. So a page starts in the
// normalized text at the counted character that has as many counted ones
// before it as the page's start has in the original. Dashes are not counted
// at all: whether a hyphen goes depends on the line break after it. A page
// with no text starts where the next one does, so no quote is cited from it.
//
// Starts must be in order, as ingest gives them.
func normalizedPageStarts(orig string, starts []int, norm string) []int {
	if len(starts) == 0 {
		return nil
	}
	before := make([]int, len(starts)) // counted characters of orig before each start
	n, i := 0, 0
	for off, r := range orig {
		for i < len(starts) && starts[i] <= off {
			before[i] = n
			i++
		}
		if countedInPages(r) {
			n++
		}
	}
	for ; i < len(starts); i++ {
		before[i] = n
	}

	out := make([]int, len(starts))
	n, i = 0, 0
	for off, r := range norm {
		if !countedInPages(r) {
			continue
		}
		for i < len(starts) && before[i] <= n {
			out[i] = off
			i++
		}
		n++
	}
	for ; i < len(starts); i++ {
		out[i] = len(norm)
	}
	return out
}

// countedInPages: the characters normalization never drops.
func countedInPages(r rune) bool {
	return !unicode.IsSpace(r) && !unicode.IsControl(r) && !isInvisible(r) && !isDash(r)
}

// indexPhrase is strings.Index from byte from that only accepts a match not
// glued to the letters or digits around it: "ион" is not in
// "функционирование". An edge of the phrase that is itself punctuation needs
// no boundary.
func indexPhrase(s, phrase string, from int) int {
	if phrase == "" {
		return -1
	}
	first, _ := utf8.DecodeRuneInString(phrase)
	last, _ := utf8.DecodeLastRuneInString(phrase)
	for from <= len(s)-len(phrase) {
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
