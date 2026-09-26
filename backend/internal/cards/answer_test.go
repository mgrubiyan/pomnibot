package cards

import "testing"

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
		{"Как называется период перед делением?", "профаза", false},           // not in the quote
		{"Как называется период перед делением?", "input", false},             // the kind's name
		{"Какую долю цикла занимает интерфаза?", "интерфаза", false},          // given in the question
		{"Что происходит с ДНК в S-периоде?", "это", false},                   // no content words
		{"Какая молекула удваивается в S-периоде?", "ДНК удваивается", false}, // "удваивается" not in the quote
	}
	for _, tt := range tests {
		c := modelCard{Kind: KindChoice, Question: tt.question, Answer: tt.answer}
		if got := answerSupported(c, quote); got != tt.want {
			t.Errorf("answerSupported(%q → %q) = %v, want %v", tt.question, tt.answer, got, tt.want)
		}
	}
}
