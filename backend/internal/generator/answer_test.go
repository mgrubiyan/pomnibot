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
