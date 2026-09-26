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
	titleMarkersMin  = 3    // distinct title-page words, at least one of them strong
	titleMaxAvgLine  = 60   // title pages are short lines; a paragraph about a university is not

	// A contents entry or a reference is a short line. A longer one is a
	// paragraph (normalization joins wrapped lines), whatever it ends with.
	maxTOCLine = 100
	maxBibLine = 400
	// A reference needs one strong cue (ISBN, URL) or this many weak ones:
	// "равен 2 с." alone is physics, "— М.: Мир, 1994. — 540 с." is a book.
	minBibCues = 2

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
	// "Глава 1. Клетка ........ 12", "1.2 Митоз … 14".
	reTOCLeader = regexp.MustCompile(`(?:\.{3,}|…+|_{3,})\s*\d{1,4}$`)
	// "Введение 3" is a contents entry only under a contents heading: without
	// one, "Крещение Руси — 988" is a date, not a page.
	reTOCBare = regexp.MustCompile(`\s\d{1,3}$`)
	reTOCHead = regexp.MustCompile(`(?i)^(содержание|оглавление|contents)\.?$`)

	reBibStrong = regexp.MustCompile(`(?i)ISBN|URL:|дата обращения|режим доступа|\s//\s|doi:`)
	// Parts of a GOST-style reference, each common enough in prose on its own.
	reBibWeak = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|\s)(?:М|Л|СПб|Спб|Минск|Киев|Казань|Новосибирск|Екатеринбург)\.?\s*:\s*\S`), // "М.: Мир"
		regexp.MustCompile(`(?i)(?:^|[^\p{L}])изд(?:\.|ательство)`),                                          // "изд.", "Издательство"
		regexp.MustCompile(`\d+\s*[сcp]\.\s*$`),                   // "540 с.", "1267 p."
		regexp.MustCompile(`(?:19|20)\d{2}\.\s*[—–-]`),            // "1994. —"
		regexp.MustCompile(`^\d+\.\s*\p{Lu}\p{Ll}+,?\s+\p{Lu}\.`), // "1. Альбертс Б."
		regexp.MustCompile(`[—–-]\s*Т\.\s*\d`),                    // "— Т. 2"
	}
	reBibHead = regexp.MustCompile(`(?i)^(список (использованной |рекомендуемой )?литературы|(рекомендуемая |основная |дополнительная )?литература|библиографический список|библиография|список источников|источники|references|bibliography)\.?:?$`)

	// Strong markers hardly occur outside a title page; weak ones do
	// ("функциональные группы", a lecture about universities), so they only
	// add up.
	titleMarkersStrong = []string{
		"министерство", "федеральное государственное", "выполнил", "проверил",
		"научный руководитель", "курсовая работа", "реферат",
	}
	titleMarkersWeak = []string{
		"университет", "институт", "кафедра", "факультет", "студент", "группы",
		"по дисциплине", "конспект лекций", "учебное пособие",
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
	var tocHead, bibHead bool
	for _, line := range lines {
		tocHead = tocHead || reTOCHead.MatchString(line)
		bibHead = bibHead || reBibHead.MatchString(line)
	}
	var total, tocRunes, bibRunes int
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		total += n
		if tocLine(line, tocHead) {
			tocRunes += n
		}
		if bibLine(line) {
			bibRunes += n
		}
	}
	avgLine := float64(total) / float64(len(lines))

	if avgLine < titleMaxAvgLine {
		if strong, all := countTitleMarkers(text); strong > 0 && all >= titleMarkersMin {
			return junkTitlePage
		}
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

func tocLine(line string, headed bool) bool {
	if utf8.RuneCountInString(line) > maxTOCLine {
		return false
	}
	return reTOCLeader.MatchString(line) || headed && reTOCBare.MatchString(line)
}

func bibLine(line string) bool {
	if utf8.RuneCountInString(line) > maxBibLine {
		return false
	}
	if reBibStrong.MatchString(line) {
		return true
	}
	cues := 0
	for _, re := range reBibWeak {
		if re.MatchString(line) {
			cues++
		}
	}
	return cues >= minBibCues
}

// classifyLines tags each line. A bare page number counts as a contents
// entry only while a contents heading is in effect, that is until the first
// line that is not an entry: "Крещение Руси — 988" after the contents is a
// date. Wrapped entries need no allowance, normalization has joined them.
func classifyLines(lines []string) []lineClass {
	classes := make([]lineClass, len(lines))
	headed := false
	for i, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case reTOCHead.MatchString(line):
			classes[i], headed = lineTOC, true
		case tocLine(line, headed):
			classes[i] = lineTOC
		case reBibHead.MatchString(line) || bibLine(line):
			classes[i], headed = lineBib, false
		default:
			classes[i], headed = lineText, false
		}
	}
	return classes
}

// junkRunBreaks marks the lines where a table of contents or a bibliography
// starts or ends. Splitting starts a new fragment at each mark, so a
// bibliography right after the last section does not take that section down
// with it, and a contents page does not ride along with the introduction.
func junkRunBreaks(lines []string) []bool {
	breaks := make([]bool, len(lines)+1)
	classes := classifyLines(lines)
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

// countTitleMarkers returns how many strong and how many markers in total
// the text contains.
func countTitleMarkers(text string) (strong, all int) {
	lower := strings.ToLower(text)
	for _, m := range titleMarkersStrong {
		if strings.Contains(lower, m) {
			strong++
		}
	}
	all = strong
	for _, m := range titleMarkersWeak {
		if strings.Contains(lower, m) {
			all++
		}
	}
	return strong, all
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
