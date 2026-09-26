package cards

import (
	"errors"
	"testing"
)

const validCard = `{"kind":"flip","question":"Что такое митоз?","answer":"Непрямое деление соматических клеток.","explanation":"Так сказано в тексте.","quote":"Митоз — непрямое деление соматических клеток","topic":"Митоз"}`

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"cards":[]}`, `{"cards":[]}`},
		{"markdown block", "```json\n{\"cards\":[]}\n```", `{"cards":[]}`},
		{"markdown block without language", "```\n{\"cards\":[]}\n```", `{"cards":[]}`},
		{"prose around a block", "Вот карточки:\n```json\n{\"cards\":[]}\n```\nГотово!", `{"cards":[]}`},
		{"prose without a block", `Конечно! {"cards":[]} Обращайтесь.`, `{"cards":[]}`},
		{"unclosed block", "```json\n{\"cards\":[]}", `{"cards":[]}`},
		{"braces inside strings", `Ответ: {"cards":[{"question":"Что значит } и {?"}]}`, `{"cards":[{"question":"Что значит } и {?"}]}`},
		{"object wins over a bracket in prose", `См. [1]. {"cards":[]}`, `{"cards":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSON([]byte(tt.in))
			if err != nil {
				t.Fatalf("extractJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("extractJSON() = %s, want %s", got, tt.want)
			}
		})
	}

	for _, bad := range []string{"", "Не могу ответить.", `{"cards": [`, "```json\n{broken}\n```"} {
		if got, err := extractJSON([]byte(bad)); err == nil {
			t.Errorf("extractJSON(%q) = %s, want error", bad, got)
		}
	}
}

func TestParseAnswer(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cards, invalid, err := parseAnswer([]byte(`{"cards":[`+validCard+`]}`), 3)
		if err != nil || invalid != 0 || len(cards) != 1 {
			t.Fatalf("got %d cards, %d invalid, err %v", len(cards), invalid, err)
		}
		c := cards[0]
		if c.Kind != KindFlip || c.Question != "Что такое митоз?" || c.Quote == "" {
			t.Errorf("card = %+v", c)
		}
	})

	t.Run("empty array is a valid answer", func(t *testing.T) {
		cards, invalid, err := parseAnswer([]byte(`{"cards":[]}`), 3)
		if err != nil || invalid != 0 || len(cards) != 0 {
			t.Fatalf("got %d cards, %d invalid, err %v", len(cards), invalid, err)
		}
	})

	t.Run("bare array", func(t *testing.T) {
		cards, _, err := parseAnswer([]byte(`[`+validCard+`]`), 3)
		if err != nil || len(cards) != 1 {
			t.Fatalf("got %d cards, err %v", len(cards), err)
		}
	})

	t.Run("cards breaking the schema are dropped one by one", func(t *testing.T) {
		answer := `{"cards":[
			` + validCard + `,
			{"kind":"table","question":"Q","answer":"A","explanation":"E","quote":"Q","topic":"T"},
			{"kind":"flip","question":"Без цитаты","answer":"A","explanation":"E","topic":"T"},
			{"kind":"flip","question":"Пустой ответ","answer":"  ","explanation":"E","quote":"Q","topic":"T"},
			{"kind":"input","question":42,"answer":"A","explanation":"E","quote":"Q","topic":"T"},
			{"kind":"boolean","question":"Митоз — прямое деление","answer":"может быть","explanation":"E","quote":"Q","topic":"T"},
			"не объект"
		]}`
		cards, invalid, err := parseAnswer([]byte(answer), 10)
		if err != nil {
			t.Fatalf("parseAnswer() error = %v", err)
		}
		if len(cards) != 1 || invalid != 6 {
			t.Errorf("got %d cards and %d invalid, want 1 and 6", len(cards), invalid)
		}
	})

	t.Run("boolean answers are normalized", func(t *testing.T) {
		for in, want := range map[string]string{"true": AnswerTrue, "Верно": AnswerTrue, "FALSE": AnswerFalse, "нет": AnswerFalse} {
			answer := `{"cards":[{"kind":"boolean","question":"Q","answer":"` + in + `","explanation":"E","quote":"Q","topic":"T"}]}`
			cards, _, err := parseAnswer([]byte(answer), 3)
			if err != nil || len(cards) != 1 || cards[0].Answer != want {
				t.Errorf("answer %q: got %+v, err %v; want %q", in, cards, err, want)
			}
		}
	})

	t.Run("more cards than allowed", func(t *testing.T) {
		answer := `{"cards":[` + validCard + `,` + validCard + `,` + validCard + `,` + validCard + `]}`
		cards, invalid, err := parseAnswer([]byte(answer), 3)
		if err != nil || len(cards) != 3 || invalid != 1 {
			t.Errorf("got %d cards, %d invalid, err %v; want 3 and 1", len(cards), invalid, err)
		}
	})

	t.Run("unusable answers are errors", func(t *testing.T) {
		for _, answer := range []string{`{"items":[]}`, `{"cards":{}}`, `"cards"`, `Извините, не могу.`} {
			if _, _, err := parseAnswer([]byte(answer), 3); err == nil {
				t.Errorf("parseAnswer(%q) succeeded, want error", answer)
			}
		}
		if _, _, err := parseAnswer([]byte(`{"items":[]}`), 3); !errors.Is(err, errNoCards) {
			t.Errorf("missing cards: err = %v, want errNoCards", err)
		}
	})
}

func TestCardSchemaIsValidJSON(t *testing.T) {
	schema := cardSchema(3)
	if _, err := extractJSON(schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
}
