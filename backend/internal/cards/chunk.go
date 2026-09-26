package cards

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// chunk is a fragment of the normalized document text. One model call is
// made per chunk, and card quotes are checked against its Text.
type chunk struct {
	Index int // position among all fragments, 0-based; SourceRef shows Index+1
	Start int // byte offsets in the normalized text: Text == text[Start:End]
	End   int
	Text  string
}

type span struct{ start, end int }

// splitChunks cuts text into fragments of at most size characters on
// paragraph boundaries. A paragraph longer than a fragment is cut on
// sentences, a sentence longer than a fragment on words. Each fragment after
// the first starts with up to overlap characters of the previous one, aligned
// to a sentence or at least a word, so that a definition split by the cut is
// still whole in one of the two.
//
// A table of contents or a bibliography always starts and ends a fragment
// (see junkRunBreaks): the junk filter then drops exactly that and nothing
// around it. No overlap is carried across such a break.
func splitChunks(text string, size, overlap int) []chunk {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	overlap = max(0, min(overlap, size/2))
	segs := segments(text, size-overlap)

	var chunks []chunk
	for i := 0; i < len(segs); {
		start := segs[i].start
		if n := len(chunks); n > 0 && overlap > 0 && !segs[i].hardBreak {
			start = overlapStart(text, chunks[n-1].Start, start, overlap)
		}
		j := i
		for j+1 < len(segs) && !segs[j+1].hardBreak &&
			utf8.RuneCountInString(text[start:segs[j+1].end]) <= size {
			j++
		}
		end := segs[j].end
		chunks = append(chunks, chunk{Index: len(chunks), Start: start, End: end, Text: text[start:end]})
		i = j + 1
	}
	return chunks
}

type segment struct {
	span
	hardBreak bool // a fragment must start here
}

// segments splits text into paragraphs (non-empty lines after normalization),
// breaking any longer than limit characters into sentences, then words.
func segments(text string, limit int) []segment {
	var lines []span
	pos := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		s := span{pos, pos + len(strings.TrimRight(line, "\n"))}
		pos += len(line)
		if strings.TrimSpace(text[s.start:s.end]) != "" {
			lines = append(lines, s)
		}
	}

	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = text[l.start:l.end]
	}
	breaks := junkRunBreaks(texts)

	var out []segment
	for i, l := range lines {
		for k, piece := range splitLong(text, l, limit) {
			out = append(out, segment{span: piece, hardBreak: k == 0 && breaks[i]})
		}
	}
	return out
}

func splitLong(text string, s span, limit int) []span {
	if utf8.RuneCountInString(text[s.start:s.end]) <= limit {
		return []span{s}
	}
	var out []span
	for _, sent := range sentences(text, s) {
		if utf8.RuneCountInString(text[sent.start:sent.end]) <= limit {
			out = append(out, sent)
			continue
		}
		out = append(out, splitWords(text, sent, limit)...)
	}
	return out
}

// sentences splits a span after ".", "!", "?" or "…" followed by a space and
// a capital letter, digit, quote or dash. Requiring the capital keeps "т. е.",
// "и т. д." and "см. рис." inside their sentence.
func sentences(text string, s span) []span {
	var out []span
	start := s.start
	for i := s.start; i < s.end; {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if !strings.ContainsRune(".!?…", r) {
			continue
		}
		j := i
		for j < s.end && text[j] == ' ' {
			j++
		}
		if j == i || j >= s.end {
			continue
		}
		next, _ := utf8.DecodeRuneInString(text[j:])
		if unicode.IsUpper(next) || unicode.IsDigit(next) || isQuoteMark(next) || isDash(next) {
			out = append(out, span{start, i})
			start = j
		}
	}
	if start < s.end {
		out = append(out, span{start, s.end})
	}
	return out
}

// splitWords packs words greedily into pieces of at most limit characters. A
// single word longer than that (a URL, a formula) is cut by characters.
func splitWords(text string, s span, limit int) []span {
	var out []span
	cur := span{-1, -1}
	flush := func() {
		if cur.start >= 0 {
			out = append(out, cur)
			cur = span{-1, -1}
		}
	}
	for _, w := range words(text, s) {
		switch {
		case utf8.RuneCountInString(text[w.start:w.end]) > limit:
			flush()
			out = append(out, splitRunes(text, w, limit)...)
		case cur.start < 0:
			cur = w
		case utf8.RuneCountInString(text[cur.start:w.end]) <= limit:
			cur.end = w.end
		default:
			flush()
			cur = w
		}
	}
	flush()
	return out
}

func words(text string, s span) []span {
	var out []span
	start := -1
	for i := s.start; i < s.end; i++ {
		if text[i] == ' ' {
			if start >= 0 {
				out = append(out, span{start, i})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, span{start, s.end})
	}
	return out
}

func splitRunes(text string, s span, limit int) []span {
	var out []span
	start, n := s.start, 0
	for i := s.start; i < s.end; {
		_, size := utf8.DecodeRuneInString(text[i:])
		if n == limit {
			out = append(out, span{start, i})
			start, n = i, 0
		}
		i += size
		n++
	}
	return append(out, span{start, s.end})
}

// overlapStart picks where the next fragment begins so that it repeats at
// most overlap characters before boundary, starting at a sentence if one fits
// and at a word otherwise.
func overlapStart(text string, prevStart, boundary, overlap int) int {
	floor := boundary
	for n := 0; n < overlap && floor > prevStart; n++ {
		_, size := utf8.DecodeLastRuneInString(text[:floor])
		floor -= size
	}
	if floor == prevStart || startsSentence(text, floor) {
		return floor
	}

	firstWord := -1
	for i := floor; i < boundary; {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if r != ' ' && r != '\n' {
			continue
		}
		prev, _ := utf8.DecodeLastRuneInString(strings.TrimRight(text[:i], " \n"))
		for i < boundary && (text[i] == ' ' || text[i] == '\n') {
			i++
		}
		if i >= boundary {
			break
		}
		if r == '\n' || strings.ContainsRune(".!?…", prev) {
			return i
		}
		if firstWord < 0 {
			firstWord = i
		}
	}
	if firstWord >= 0 {
		return firstWord
	}
	return boundary
}

// startsSentence reports whether a sentence or a line begins at i.
func startsSentence(text string, i int) bool {
	if i < len(text) && (text[i] == ' ' || text[i] == '\n') {
		return false
	}
	if i == 0 {
		return true
	}
	before := text[:i]
	trimmed := strings.TrimRight(before, " ")
	if strings.HasSuffix(trimmed, "\n") {
		return true
	}
	if len(trimmed) == len(before) {
		return false // i is inside a word
	}
	prev, _ := utf8.DecodeLastRuneInString(trimmed)
	return strings.ContainsRune(".!?…", prev)
}
