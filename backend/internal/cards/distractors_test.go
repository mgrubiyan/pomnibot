package cards

import (
	"fmt"
	"slices"
	"testing"
)

func TestQuestionIndexDuplicates(t *testing.T) {
	x := newQuestionIndex()
	x.add("Что такое митоз?")
	x.add("Какие фазы выделяют в митозе?")
	x.add("Какие изменения происходят с хромосомами в профазе первого деления мейоза?")
	x.add("Что такое хромосома?")

	for _, q := range []string{
		"что такое митоз",
		"Что такое  МИТОЗ?!",
		"Что называют митозом?",
		"Какие фазы выделяют в митозе клетки?",
	} {
		if !x.isDup(q) {
			t.Errorf("%q is not detected as a duplicate", q)
		}
	}
	for _, q := range []string{
		"Что такое мейоз?",
		"Сколько хромосом в клетках человека?",
		"Какая фаза митоза самая короткая?",
		"Какие изменения происходят с хромосомами в профазе второго деления мейоза?",
		"Что такое гомологичная хромосома?",
	} {
		if x.isDup(q) {
			t.Errorf("%q is wrongly detected as a duplicate", q)
		}
	}
}

// entry is a pool answer: its text, topic and fragment position.
type entry struct {
	answer, topic string
	pos           int
}

func poolWith(entries ...entry) *distractorPool {
	p := newDistractorPool()
	for _, e := range entries {
		p.add(KindChoice, e.answer, e.topic, e.pos)
	}
	return p
}

func TestDistractorPick(t *testing.T) {
	p := poolWith(
		entry{"Митоз", "Деление клетки", 0},     // the answer itself
		entry{"Амитоз", "Деление клетки", 1},    // contains the answer
		entry{"Мейоз", "Деление клетки", 1},     // good, same topic
		entry{"Цитокинез", "Деление клетки", 2}, // good, same topic
		entry{"Апоптоз", "Гибель клетки", 3},    // good
		entry{"Интерфаза", "Клеточный цикл", 0}, // same fragment as the card
		entry{"46", "Хромосомы", 2},             // a number for a word answer
		entry{"Совокупность процессов между двумя делениями клетки", "Клеточный цикл", 4}, // far too long
		entry{"Кроссинговер", "Мейоз", 4}, // appears in the quote
	)
	card := Card{
		Kind:        KindChoice,
		Question:    "Как называется непрямое деление соматических клеток?",
		Answer:      "Митоз",
		SourceQuote: "Митоз — непрямое деление соматических клеток; кроссинговер при нём не происходит.",
		Topic:       "Деление клетки",
	}

	got := p.pick(card, 0, 3)
	want := []string{"Мейоз", "Цитокинез", "Апоптоз"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("pick() = %v, want %v", got, want)
	}

	numeric := Card{Question: "Сколько хромосом у человека?", Answer: "46", SourceQuote: "46 хромосом"}
	p.add(KindInput, "23", "Хромосомы", 5)
	if got := p.pick(numeric, 2, 3); !slices.Equal(got, []string{"23"}) {
		t.Errorf("numeric answer: pick() = %v, want [23]", got)
	}
}

func TestShuffleOptions(t *testing.T) {
	distractors := []string{"Мейоз", "Амитоз", "Цитокинез"}
	positions := make([]int, 4)
	for i := range 200 {
		q := fmt.Sprintf("Вопрос номер %d о делении клетки?", i)
		opts := shuffleOptions(q, "Митоз", distractors)
		if len(opts) != 4 {
			t.Fatalf("got %d options", len(opts))
		}
		if again := shuffleOptions(q, "Митоз", distractors); !slices.Equal(opts, again) {
			t.Fatalf("order is not deterministic for %q: %v vs %v", q, opts, again)
		}
		positions[slices.Index(opts, "Митоз")]++
	}
	for pos, n := range positions {
		// 50 expected per position; a fair shuffle stays well inside 25..75.
		if n < 25 || n > 75 {
			t.Errorf("answer at position %d in %d of 200 cards: %v", pos, n, positions)
		}
	}
	if !slices.Equal(distractors, []string{"Мейоз", "Амитоз", "Цитокинез"}) {
		t.Error("shuffleOptions modified its input")
	}
}

func TestOptionForm(t *testing.T) {
	for in, want := range map[string]string{
		"Мейоз.":             "Мейоз",
		"цитокинез":          "Цитокинез",
		"ДНК":                "ДНК",
		"  Репликация ДНК; ": "Репликация ДНК",
		"46":                 "46",
		"(2n)":               "(2n)",
		"S-период":           "S-период",
		"Na":                 "Na",
		"Гольджи":            "Гольджи",
		"мРНК":               "мРНК",
		"pH":                 "pH",
		"метафазная пластинка": "Метафазная пластинка",
	} {
		if got := optionForm(in); got != want {
			t.Errorf("optionForm(%q) = %q, want %q", in, got, want)
		}
	}
}
