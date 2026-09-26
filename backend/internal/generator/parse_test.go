package generator

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// validCard is a card of the flat form: it carries its own quote and topic.
const validCard = `{"kind":"flip","question":"Что такое митоз?","answer":"Непрямое деление соматических клеток.","explanation":"Так сказано в тексте.","quote":"Митоз — непрямое деление соматических клеток","topic":"Митоз"}`

const validFact = `{"quote":"Анафаза — самая короткая фаза митоза","topic":"Фазы митоза","cards":[
	{"kind":"choice","question":"Какая фаза митоза самая короткая?","answer":"Анафаза","explanation":"E"},
	{"kind":"input","question":"Какая фаза митоза самая короткая?","answer":"анафаза","explanation":"E"},
	{"kind":"boolean","question":"Метафаза — самая короткая фаза митоза","answer":"false","explanation":"E"},
	{"kind":"flip","question":"Чем анафаза отличается от других фаз митоза?","answer":"Она самая короткая.","explanation":"E"}]}`

func cardCount(facts []modelFact) int {
	n := 0
	for _, f := range facts {
		n += len(f.Cards)
	}
	return n
}

func TestJSONCandidates(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // first candidate
	}{
		{"plain", `{"facts":[]}`, `{"facts":[]}`},
		{"markdown block", "```json\n{\"facts\":[]}\n```", `{"facts":[]}`},
		{"markdown block without language", "```\n{\"facts\":[]}\n```", `{"facts":[]}`},
		{"prose around a block", "Вот карточки:\n```json\n{\"facts\":[]}\n```\nГотово!", `{"facts":[]}`},
		{"prose without a block", `Конечно! {"facts":[]} Обращайтесь.`, `{"facts":[]}`},
		{"unclosed block", "```json\n{\"facts\":[]}", `{"facts":[]}`},
		{"braces inside strings", `Ответ: {"facts":[{"quote":"Что значит } и {?"}]}`, `{"facts":[{"quote":"Что значит } и {?"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonCandidates([]byte(tt.in))
			if len(got) == 0 || string(got[0]) != tt.want {
				t.Errorf("jsonCandidates() first = %q, want %s", got, tt.want)
			}
		})
	}

	for _, bad := range []string{"", "Не могу ответить.", `{"facts": [`, "```json\n{broken}\n```"} {
		if got := jsonCandidates([]byte(bad)); len(got) != 0 {
			t.Errorf("jsonCandidates(%q) = %q, want none", bad, got)
		}
	}
}

func TestParseAnswer(t *testing.T) {
	t.Run("facts with cards of several kinds", func(t *testing.T) {
		facts, invalid, err := parseAnswer([]byte(`{"facts":[`+validFact+`]}`), 3)
		if err != nil || invalid != 0 || len(facts) != 1 {
			t.Fatalf("got %d facts, %d invalid, err %v", len(facts), invalid, err)
		}
		f := facts[0]
		if f.Quote == "" || f.Topic != "Фазы митоза" || len(f.Cards) != 4 {
			t.Errorf("fact = %+v", f)
		}
		if f.Cards[2].Kind != cards.KindBoolean || f.Cards[2].Answer != cards.AnswerFalse {
			t.Errorf("boolean card = %+v", f.Cards[2])
		}
	})

	t.Run("empty facts is a valid answer", func(t *testing.T) {
		facts, invalid, err := parseAnswer([]byte(`{"facts":[]}`), 3)
		if err != nil || invalid != 0 || len(facts) != 0 {
			t.Fatalf("got %d facts, %d invalid, err %v", len(facts), invalid, err)
		}
	})

	t.Run("flat cards with one quote are one fact", func(t *testing.T) {
		boolean := `{"kind":"boolean","question":"Митоз — прямое деление","answer":"false","explanation":"E","quote":"Митоз — непрямое деление соматических клеток","topic":"Митоз"}`
		other := `{"kind":"flip","question":"Что такое мейоз?","answer":"Деление.","explanation":"E","quote":"Мейоз уменьшает число хромосом вдвое","topic":"Мейоз"}`
		facts, _, err := parseAnswer([]byte(`{"cards":[`+validCard+`,`+boolean+`,`+other+`]}`), 3)
		if err != nil || len(facts) != 2 || len(facts[0].Cards) != 2 || len(facts[1].Cards) != 1 {
			t.Fatalf("got %+v, err %v; want facts of 2 and 1 cards", facts, err)
		}
	})

	t.Run("bare array", func(t *testing.T) {
		facts, _, err := parseAnswer([]byte(`[`+validFact+`]`), 3)
		if err != nil || cardCount(facts) != 4 {
			t.Fatalf("bare array of facts: got %d cards, err %v", cardCount(facts), err)
		}
		facts, _, err = parseAnswer([]byte(`[`+validCard+`]`), 3)
		if err != nil || cardCount(facts) != 1 {
			t.Fatalf("bare array of cards: got %d cards, err %v", cardCount(facts), err)
		}
	})

	t.Run("bare array of cards inside prose", func(t *testing.T) {
		facts, _, err := parseAnswer([]byte(`Вот карточки: [`+validCard+`]`), 3)
		if err != nil || cardCount(facts) != 1 {
			t.Fatalf("got %d cards, err %v", cardCount(facts), err)
		}
	})

	t.Run("stray value in prose before the answer", func(t *testing.T) {
		facts, _, err := parseAnswer([]byte(`См. [1]. {"facts":[`+validFact+`]}`), 3)
		if err != nil || cardCount(facts) != 4 {
			t.Fatalf("got %d cards, err %v", cardCount(facts), err)
		}
	})

	t.Run("answers written as JSON booleans and numbers", func(t *testing.T) {
		answer := `{"facts":[{"quote":"Q","topic":"T","cards":[
			{"kind":"boolean","question":"Q","answer":true,"explanation":"E"},
			{"kind":"input","question":"Q","answer":1961,"explanation":"E"}]}]}`
		facts, invalid, err := parseAnswer([]byte(answer), 3)
		if err != nil || invalid != 0 || cardCount(facts) != 2 {
			t.Fatalf("got %+v, %d invalid, err %v", facts, invalid, err)
		}
		if facts[0].Cards[0].Answer != cards.AnswerTrue || facts[0].Cards[1].Answer != "1961" {
			t.Errorf("answers = %q, %q", facts[0].Cards[0].Answer, facts[0].Cards[1].Answer)
		}
	})

	t.Run("cards breaking the schema are dropped one by one", func(t *testing.T) {
		answer := `{"facts":[{"quote":"Q","topic":"T","cards":[
			{"kind":"flip","question":"Q","answer":"A","explanation":"E"},
			{"kind":"table","question":"Q","answer":"A","explanation":"E"},
			{"kind":"flip","question":"Без объяснения","answer":"A"},
			{"kind":"boolean","question":"Q","answer":"может быть","explanation":"E"}]},
			{"topic":"Без цитаты","cards":[
				{"kind":"flip","question":"Q","answer":"A","explanation":"E"},
				{"kind":"flip","question":"Q2","answer":"A","explanation":"E"}]},
			"не объект"]}`
		facts, invalid, err := parseAnswer([]byte(answer), 10)
		if err != nil {
			t.Fatalf("parseAnswer() error = %v", err)
		}
		// 3 bad cards in the first fact, 2 cards of the fact without a quote,
		// 1 for the item that is not an object.
		if cardCount(facts) != 1 || invalid != 6 {
			t.Errorf("got %d cards and %d invalid, want 1 and 6", cardCount(facts), invalid)
		}
	})

	t.Run("boolean answers are normalized", func(t *testing.T) {
		for in, want := range map[string]string{"true": cards.AnswerTrue, "Верно": cards.AnswerTrue, "FALSE": cards.AnswerFalse, "нет": cards.AnswerFalse} {
			answer := `{"facts":[{"quote":"Q","topic":"T","cards":[{"kind":"boolean","question":"Q","answer":"` + in + `","explanation":"E"}]}]}`
			facts, _, err := parseAnswer([]byte(answer), 3)
			if err != nil || cardCount(facts) != 1 || facts[0].Cards[0].Answer != want {
				t.Errorf("answer %q: got %+v, err %v; want %q", in, facts, err, want)
			}
		}
	})

	t.Run("more facts or cards than allowed", func(t *testing.T) {
		answer := `{"facts":[` + validFact + `,` + validFact + `]}`
		facts, invalid, err := parseAnswer([]byte(answer), 1)
		if err != nil || len(facts) != 1 || invalid != 4 {
			t.Errorf("got %d facts, %d invalid, err %v; want 1 and 4", len(facts), invalid, err)
		}
		five := `{"facts":[{"quote":"Q","topic":"T","cards":[` +
			`{"kind":"flip","question":"1","answer":"A","explanation":"E"},{"kind":"flip","question":"2","answer":"A","explanation":"E"},` +
			`{"kind":"flip","question":"3","answer":"A","explanation":"E"},{"kind":"flip","question":"4","answer":"A","explanation":"E"},` +
			`{"kind":"flip","question":"5","answer":"A","explanation":"E"}]}]}`
		facts, invalid, err = parseAnswer([]byte(five), 3)
		if err != nil || cardCount(facts) != maxCardsPerFact || invalid != 1 {
			t.Errorf("got %d cards, %d invalid, err %v; want %d and 1", cardCount(facts), invalid, err, maxCardsPerFact)
		}
	})

	t.Run("unusable answers are errors", func(t *testing.T) {
		for _, answer := range []string{`{"items":[]}`, `{"facts":{}}`, `"facts"`, `Извините, не могу.`} {
			if _, _, err := parseAnswer([]byte(answer), 3); err == nil {
				t.Errorf("parseAnswer(%q) succeeded, want error", answer)
			}
		}
		if _, _, err := parseAnswer([]byte(`{"items":[]}`), 3); !errors.Is(err, errNoFacts) {
			t.Errorf("missing facts: err = %v, want errNoFacts", err)
		}
	})
}

func TestCardSchemaIsValidJSON(t *testing.T) {
	if schema := cardSchema(3); !json.Valid(schema) {
		t.Fatalf("schema is not valid JSON: %s", schema)
	}
}
