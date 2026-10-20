package generator

import (
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

func TestAnswerSupported(t *testing.T) {
	const quote = "Интерфаза делится на три периода: пресинтетический (G1), синтетический (S) и постсинтетический (G2). " +
		"В S-периоде происходит репликация ДНК. У большинства клеток интерфаза занимает около 90 % времени цикла. " +
		"В животных клетках цитоплазма разделяется перетяжкой."
	tests := []struct {
		question, answer string
		want             bool
	}{
		{"Какой период предшествует синтезу ДНК?", "пресинтетический (G1)", true},
		{"Перечислите периоды интерфазы.", "G1, S, G2", true},
		{"В каком периоде происходит репликация ДНК?", "S-период", true},
		{"Сколько времени цикла занимает основной период?", "около 90%", true},
		{"Чем разделяется цитоплазма животной клетки?", "перетяжка", true},
		{"Как называется период перед делением?", "профаза", false},               // not in the quote
		{"Как называется период перед делением?", "input", false},                 // the kind's name
		{"Какую долю цикла занимает интерфаза?", "интерфаза", false},              // given in the question
		{"Что происходит с ДНК в S-периоде?", "это", false},                       // no content words
		{"Какая молекула удваивается в S-периоде?", "ДНК удваивается", false},     // "удваивается" not in the quote
		{"Что контролирует переход между периодами?", "контрольные точки", false}, // not in this quote
	}
	for _, tt := range tests {
		c := modelCard{Kind: cards.KindChoice, Question: tt.question, Answer: tt.answer}
		if got := answerSupported(c, quote); got != tt.want {
			t.Errorf("answerSupported(%q → %q) = %v, want %v", tt.question, tt.answer, got, tt.want)
		}
	}
}

func TestAnswerSupportedAcrossWordForms(t *testing.T) {
	// Seen live: a correct card rejected because «точки» ≠ «точках» by stem.
	const quote = "Переход между периодами цикла контролируется в контрольных точках"
	c := modelCard{Kind: cards.KindChoice, Question: "Что контролирует переходы между периодами клеточного цикла?", Answer: "контрольные точки"}
	if !answerSupported(c, quote) {
		t.Error("«контрольные точки» is in «в контрольных точках», and «контролирует» is another word")
	}
}

func TestSameWord(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"точки", "точках", true},
		{"интерфаза", "интерфазы", true},
		{"клетки", "клеток", true},
		{"период", "периоде", true},
		{"перетяжка", "перетяжкой", true},
		{"митоз", "мейоз", false},
		{"контрольные", "контролирует", false},
		{"g1", "g2", false},
		{"90", "90", true},
	} {
		if got := sameWord([]rune(tt.a), []rune(tt.b)); got != tt.want {
			t.Errorf("sameWord(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// Code answers are checked as whole strings: "::" has no words, a command
// may be made of words from its question, and pdftotext spaces out the
// letters of listings ("c o u t . i m b u e").
func TestAnswerSupportedForCode(t *testing.T) {
	const quote = "Метод imbue назначает потоку новую локаль: c o u t . i m b u e ( l o c ) ; " +
		"Оператор :: обращается к члену пространства имён: ns2::ns1::a. " +
		"Компоновка: g++ ./main.obj ./hello.obj -o program.exe. Директива #define pi 4 заменяет pi на 4."
	tests := []struct {
		question, answer string
		want             bool
	}{
		{"Какой метод назначает потоку новую локаль?", "imbue", true},
		{"Какой оператор обращается к члену пространства имён?", "::", true},
		{"Как обратиться к переменной a из ns1 внутри ns2?", "ns2::ns1::a", true},
		{"Запиши команду компоновки main.obj и hello.obj в program.exe через g++.", "g++ ./main.obj ./hello.obj -o program.exe", true},
		{"Как вывести значение через поток?", "cout.imbue(loc)", true},       // spaced out in the notes
		{"Какой функцией задают локаль программы?", "setlocale", false},      // not in the quote
		{"Что делает метод imbue с потоком?", "imbue", false},                // given in the question
		{"На какое число заменится pi директивой #define pi 4?", "4", false}, // given in the question
		{"Какой символ начинает директиву?", "s", false},                     // one letter is in any text
	}
	for _, tt := range tests {
		c := modelCard{Kind: cards.KindChoice, Question: tt.question, Answer: tt.answer}
		if got := answerSupported(c, quote); got != tt.want {
			t.Errorf("answerSupported(%q → %q) = %v, want %v", tt.question, tt.answer, got, tt.want)
		}
	}
}

// A flip answer and a true claim rephrase the quote, so most of their words
// come from it. One that shares few words with it is made up («denaturation»
// of «Денатурация») or belongs to another fact: the card would show a quote
// that does not prove it.
func TestClaimSupported(t *testing.T) {
	const metals = "Простые вещества делят на металлы и неметаллы. К металлам относятся железо, медь, натрий и алюминий."
	const row = "Число делений | одно | два"
	for _, tc := range []struct {
		name, claim, quote string
		want               bool
	}{
		{"a restatement", "К металлам относятся железо и медь.", metals, true},
		{"a paraphrase", "Железо и алюминий — металлы.", metals, true},
		{"a row of a table, headers from the question", "При митозе происходит одно деление, а при мейозе — два.", row, true},
		{"made up", "denaturation", "Денатурация", false},
		{"another fact", "Металлы хорошо проводят электрический ток и тепло.", metals, false},
		{"another fragment", "Кинетической энергией обладает движущееся тело.", metals, false},
		{"no content words", "Это.", metals, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimSupported(tc.claim, tc.quote); got != tc.want {
				t.Errorf("claimSupported(%q) = %v, want %v", tc.claim, got, tc.want)
			}
		})
	}
}
