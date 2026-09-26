package cards

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Junk filter thresholds. A fragment that looks like a title page, a table of
// contents, a bibliography or a run of formulas and numbers is dropped before
// the model call: with one GigaChat thread every saved call is 10–30 seconds
// off the wait.
//
// Mistakes cost differently: a fragment dropped by mistake loses material for
// good, while junk that slips through costs one call that returns an empty
// array. The thresholds lean towards letting text through, and table-of-contents
// and bibliography shares are weighted by characters, not lines, so a long
// paragraph next to a short list still passes.
const (
	minLetters       = 80   // fewer letters than this: nothing to ask about
	minLetterRatio   = 0.5  // letters / non-space characters; formulas and number tables fall below
	maxDigitDotRatio = 0.3  // (digits + dots) / non-space characters
	minAvgLineLen    = 16   // characters per line, checked from minLinesForAvg lines: bare headings, formulas
	minLinesForAvg   = 6    //
	minLinesForShare = 3    // below this, table-of-contents and bibliography shares are not checked
	tocShare         = 0.5  // share of text in lines ending with a page number
	tocShareHeaded   = 0.3  // the same when the fragment has a "Содержание" heading
	bibShare         = 0.4  // share of text in lines that look like bibliography entries
	bibShareHeaded   = 0.25 // the same under a "Список литературы" heading
	titleMarkersMin  = 3    // distinct title-page words: министерство, кафедра, выполнил...
	titleMaxAvgLine  = 60   // title pages are short lines; a paragraph about a university is not

	// A run of this many table-of-contents or bibliography lines (a heading
	// counts) gets fragments of its own, so that it neither drags real text
	// into the junk bin nor slips through diluted by it. One stray line inside
	// the run is tolerated: a wrapped entry, a page number.
	minTOCRun = 3
	minBibRun = 2
)

// Junk reasons, used as Stats.FilteredBy keys.
const (
	junkTooShort     = "too_short"
	junkTitlePage    = "title_page"
	junkTOC          = "toc"
	junkBibliography = "bibliography"
	junkFewLetters   = "few_letters"
	junkDigitsDots   = "digits_dots"
	junkShortLines   = "short_lines"
)

var (
	// "Глава 1. Клетка ........ 12", "1.2 Митоз … 14", "Введение 3".
	reTOCLine = regexp.MustCompile(`(?:\.{3,}|…+|_{3,}|\s)\s*\d{1,4}$`)
	reTOCHead = regexp.MustCompile(`(?i)^(содержание|оглавление|contents)\.?$`)

	// Typical parts of a GOST-style reference: "— М.: Мир, 1994. — 517 с.",
	// "ISBN", "URL:", "// Вестник", "(дата обращения: ...)".
	reBibLine = regexp.MustCompile(`(?i)(?:^|\s)(?:[МЛ]|СПб|Спб|Минск|Киев|Казань)\.?\s*:|ISBN|URL:|дата обращения|режим доступа|\bизд(?:\.|ательство)|\d+\s*[сc]\.\s*$|\d+\s*p\.\s*$|\s//\s|(?:19|20)\d{2}\.\s*[—–-]`)
	reBibHead = regexp.MustCompile(`(?i)^(список (использованной |рекомендуемой )?литературы|(рекомендуемая |основная |дополнительная )?литература|библиографический список|библиография|список источников|источники|references|bibliography)\.?:?$`)

	titleMarkers = []string{
		"министерство", "федеральное государственное", "университет", "институт",
		"кафедра", "факультет", "выполнил", "проверил", "студент", "группы",
		"научный руководитель", "реферат", "курсовая работа", "по дисциплине",
		"конспект лекций", "учебное пособие",
	}
)

// junkReason returns why a fragment should not be sent to the model, or ""
// if it should.
func junkReason(text string) string {
	var letters, digitsDots, nonSpace int
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			continue
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r) || r == '.' || r == '…' || r == '_' || r == '·':
			digitsDots++
		}
		nonSpace++
	}
	if letters < minLetters {
		return junkTooShort
	}

	lines := nonEmptyLines(text)
	var total, tocRunes, bibRunes int
	var tocHead, bibHead bool
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		total += n
		if reTOCLine.MatchString(line) {
			tocRunes += n
		}
		if reBibLine.MatchString(line) {
			bibRunes += n
		}
		tocHead = tocHead || reTOCHead.MatchString(line)
		bibHead = bibHead || reBibHead.MatchString(line)
	}
	avgLine := float64(total) / float64(len(lines))

	if avgLine < titleMaxAvgLine && countTitleMarkers(text) >= titleMarkersMin {
		return junkTitlePage
	}
	if len(lines) >= minLinesForShare {
		if share(tocRunes, total) >= pick(tocHead, tocShareHeaded, tocShare) {
			return junkTOC
		}
		if share(bibRunes, total) >= pick(bibHead, bibShareHeaded, bibShare) {
			return junkBibliography
		}
	}
	if share(letters, nonSpace) < minLetterRatio {
		return junkFewLetters
	}
	if share(digitsDots, nonSpace) > maxDigitDotRatio {
		return junkDigitsDots
	}
	if len(lines) >= minLinesForAvg && avgLine < minAvgLineLen {
		return junkShortLines
	}
	return ""
}

type lineClass int

const (
	lineText lineClass = iota
	lineTOC
	lineBib
)

func classifyLine(line string) lineClass {
	switch {
	case reTOCHead.MatchString(line) || reTOCLine.MatchString(line):
		return lineTOC
	case reBibHead.MatchString(line) || reBibLine.MatchString(line):
		return lineBib
	}
	return lineText
}

// junkRunBreaks marks the lines where a table of contents or a bibliography
// starts or ends. Splitting starts a new fragment at each mark, so a
// bibliography right after the last section does not take that section down
// with it, and a contents page does not ride along with the introduction.
func junkRunBreaks(lines []string) []bool {
	breaks := make([]bool, len(lines)+1)
	classes := make([]lineClass, len(lines))
	for i, line := range lines {
		classes[i] = classifyLine(strings.TrimSpace(line))
	}
	for i := 0; i < len(lines); {
		c := classes[i]
		if c == lineText {
			i++
			continue
		}
		// Extend the run over lines of the same class, stepping over one
		// stray line when the class resumes right after it.
		j, count := i, 0
		for j < len(lines) {
			if classes[j] == c {
				count++
				j++
				continue
			}
			if j+1 < len(lines) && classes[j+1] == c {
				j++
				continue
			}
			break
		}
		need := minBibRun
		if c == lineTOC {
			need = minTOCRun
		}
		if count >= need {
			breaks[i], breaks[j] = true, true
		}
		i = j
	}
	return breaks[:len(lines)]
}

func nonEmptyLines(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func countTitleMarkers(text string) int {
	lower := strings.ToLower(text)
	n := 0
	for _, m := range titleMarkers {
		if strings.Contains(lower, m) {
			n++
		}
	}
	return n
}

func share(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func pick(cond bool, yes, no float64) float64 {
	if cond {
		return yes
	}
	return no
}
