package generator

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Explanation checks. The prompt asks for an explanation from the fragment
// alone, yet models fill it with what they know of the subject, and when told
// not to, they write "В конспекте указано, что…" instead. Both are fixed
// here, sentence by sentence:
//
//   - a lead-in naming the notes is cut off: «В конспекте указано, что синус
//     30° равен 1/2.» becomes «Синус 30° равен 1/2.», and so is a trailing
//     «, что указано в таблице значений»;
//   - a sentence still about the notes («Формулировка приведена в
//     конспекте.») is dropped;
//   - a sentence whose content words the fragment mostly lacks is dropped: it
//     brings in knowledge from outside the notes.
//
// What is left may be empty: the quote under the answer still shows where
// the answer comes from.

// minSupport is the share of a sentence's content words the fragment must
// have, up to endings, for the sentence to stay.
const minSupport = 0.6

var (
	// reNotesLeadIn: «В конспекте (чётко) указано, что», «Согласно фрагменту,».
	reNotesLeadIn = regexp.MustCompile(`(?i)^(?:(?:в|во)\s+(?:конспекте|тексте|фрагменте|лекции|материале` +
		`|таблице(?:\s+значений)?|определении|начале(?:\s+раздела)?)` +
		`|согласно\s+(?:конспекту|тексту|фрагменту|лекции|материалу)` +
		`|(?:конспект|текст|фрагмент|лекция)\s+(?:утверждает|говорит|указывает|сообщает|гласит|объясняет))` +
		`(?:\s+(?:чётко|четко|прямо|явно|также|ясно|отдельно))?` +
		`(?:\s+(?:указано|сказано|говорится|отмечено|написано|утверждается|приводится|приведено|приведена|приведены|` +
		`упоминается|описано|описывается|объясняется|дано|даётся|дается|подчёркивается|подчеркивается))?` +
		`\s*,?\s*(?:что\s+)?`)
	// reSourceTail: «, что указано в таблице …», «, как сказано выше»,
	// «, согласно определению в конспекте» at the end of a sentence.
	reSourceTail = regexp.MustCompile(`(?i)\s*,\s*(?:(?:что|как)\s+(?:это\s+)?(?:(?:прямо|дословно|явно|чётко|четко|ясно|также)\s+)?(?:указано|сказано|говорится|отмечено|написано|` +
		`приведено|показано|видно|следует)|согласно)(?:\s+[^,.!?]*)?([.!?…]?)$`)
	// reAboutNotes: a sentence that still talks about the notes, not the
	// subject. "Текст" alone is not enough: «в тексте романа» is the subject
	// of a literature lecture.
	reAboutNotes = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:конспект|фрагмент|цитат|лекци)\p{L}*` +
		`|(?:указан|сказан|написан|приведен|отмечен)\p{L}*\s+(?:(?:прямо|дословно|явно)\s+)?в\s+тексте` +
		`|(?:^|[^\p{L}])(?:в|во)\s+начале\s+[^.!?]*(?:указан|приведен|дан)`)
	// A notation qualifier limits a statement to the source's conventions;
	// unlike commentary about the notes, it is part of the fact's meaning.
	reNotationScope = regexp.MustCompile(`(?i)^в\s+(?:обозначениях|нотации)\s+(?:лекции|конспекта)\s+`)
)

// explanationStopWords carry no knowledge of their own: a sentence is judged
// by the rest.
var explanationStopWords = map[string]bool{
	"который": true, "которая": true, "которое": true, "которые": true, "которого": true, "которой": true,
	"которых": true, "которым": true, "также": true, "поэтому": true, "потому": true, "этом": true,
	"этого": true, "этой": true, "этих": true, "тому": true, "того": true, "так": true, "тоже": true,
	"именно": true, "только": true, "всего": true, "всех": true, "весь": true, "вся": true, "все": true,
	"более": true, "менее": true, "очень": true, "может": true, "могут": true, "можно": true,
	"нужно": true, "есть": true, "был": true, "была": true, "было": true, "были": true, "будет": true,
	"его": true, "ее": true, "их": true, "он": true, "она": true, "оно": true, "они": true,
	"свой": true, "своей": true, "свою": true, "своих": true, "себя": true, "между": true,
	"после": true, "перед": true, "через": true, "без": true, "если": true, "чтобы": true,
	"значит": true, "равен": true, "равна": true, "равно": true, "равны": true, "поскольку": true,
	"однако": true, "тогда": true, "здесь": true, "таким": true, "образом": true, "т": true, "е": true,
}

// mathWords: notes write the abbreviation, explanations the word.
var mathWords = map[string]string{
	"sin": "синус", "cos": "косинус", "tg": "тангенс", "tan": "тангенс", "ctg": "котангенс", "cot": "котангенс",
	"log": "логарифм", "lg": "логарифм", "ln": "логарифм", "lim": "предел",
}

// cleanExplanation returns the explanation without lead-ins naming the
// notes and without sentences about the notes or from outside them.
func cleanExplanation(explanation, fragment string) string {
	have := contentWords(fragment)
	for _, w := range have {
		if word, ok := mathWords[string(w)]; ok {
			have = append(have, []rune(word))
		}
	}
	var kept []string
	for _, s := range explanationSentences(explanation) {
		if body := reNotesLeadIn.ReplaceAllString(s, ""); body != s {
			if s = upperFirst(strings.TrimSpace(body)); s == "" {
				continue
			}
		}
		if s = reSourceTail.ReplaceAllString(s, "$1"); s != "" && !strings.ContainsRune(".!?…", []rune(s)[len([]rune(s))-1]) {
			s += "."
		}
		body := reNotationScope.ReplaceAllString(s, "")
		if reAboutNotes.MatchString(body) || !supported(body, have) {
			continue
		}
		kept = append(kept, s)
	}
	return strings.Join(kept, " ")
}

// supported reports whether the fragment has at least minSupport of the
// sentence's content words. A sentence without any, a formula, passes.
func supported(sentence string, have [][]rune) bool {
	var total, found int
	for _, w := range contentWords(sentence) {
		if explanationStopWords[string(w)] {
			continue
		}
		total++
		if slices.ContainsFunc(have, func(h []rune) bool { return sameWord(w, h) }) {
			found++
		}
	}
	return total == 0 || float64(found) >= minSupport*float64(total)
}

// explanationSentences splits text after ".", "!", "?" or "…" followed by a space and a
// capital letter or a digit, so "т. е." and "1/2." inside a sentence stay.
func explanationSentences(text string) []string {
	var out []string
	rs := []rune(strings.TrimSpace(text))
	start := 0
	for i := 0; i < len(rs); i++ {
		if !strings.ContainsRune(".!?…", rs[i]) || i+2 >= len(rs) || !unicode.IsSpace(rs[i+1]) {
			continue
		}
		if next := rs[i+2]; unicode.IsUpper(next) || unicode.IsDigit(next) {
			out = append(out, strings.TrimSpace(string(rs[start:i+1])))
			start = i + 2
		}
	}
	if rest := strings.TrimSpace(string(rs[start:])); rest != "" {
		out = append(out, rest)
	}
	return out
}
