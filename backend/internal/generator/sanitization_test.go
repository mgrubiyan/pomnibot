package generator

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeChoiceCardRemovesEmptyStringsAndDuplicates(t *testing.T) {
	c := ChoiceCard{
		CardBase:     CardBase{Question: "Which is correct?", Source: "Source"},
		RightAnswers: []string{" Option A ", "", "  ", "Option A", "Option B"},
		WrongAnswers: []string{" Option C ", "Option A", "", " Option D ", "Option C"},
	}
	cleaned := sanitizeChoiceCard(c)
	if !reflect.DeepEqual(cleaned.RightAnswers, []string{"Option A", "Option B"}) {
		t.Fatalf("unexpected RightAnswers: %v", cleaned.RightAnswers)
	}
	if !reflect.DeepEqual(cleaned.WrongAnswers, []string{"Option C", "Option D"}) {
		t.Fatalf("unexpected WrongAnswers: %v", cleaned.WrongAnswers)
	}
}

func TestSanitizeTableCardRemovesEmptyRows(t *testing.T) {
	c := TableCard{
		CardBase: CardBase{Question: "Sort", Source: "Source"},
		Columns: []Column{
			{Name: " Cat 1 ", Rows: []string{" A ", "", "  ", "A", "B"}},
			{Name: " Cat 2 ", Rows: []string{" C ", " "}},
		},
	}
	cleaned := sanitizeTableCard(c)
	if cleaned.Columns[0].Name != "Cat 1" || !reflect.DeepEqual(cleaned.Columns[0].Rows, []string{"A", "B"}) {
		t.Fatalf("unexpected Column 0: %+v", cleaned.Columns[0])
	}
	if cleaned.Columns[1].Name != "Cat 2" || !reflect.DeepEqual(cleaned.Columns[1].Rows, []string{"C"}) {
		t.Fatalf("unexpected Column 1: %+v", cleaned.Columns[1])
	}
}

func TestSameQuestionWithDifferentAnswersAllowedAcrossAndWithinFacts(t *testing.T) {
	fact1 := Fact{
		Name:  "Fact 1",
		Pages: []int{1},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			InputCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			ChoiceCard{CardBase: CardBase{Question: "Common Question", Source: "S1"}, RightAnswers: []string{"R1"}, WrongAnswers: []string{"W1"}},
			ChoiceCard{CardBase: CardBase{Question: "Common Question", Source: "S1"}, RightAnswers: []string{"R2"}, WrongAnswers: []string{"W2"}},
		},
	}
	fact2 := Fact{
		Name:  "Fact 2",
		Pages: []int{2},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "Q2", Source: "S2"}, Answer: "A2"},
			InputCard{CardBase: CardBase{Question: "Q2", Source: "S2"}, Answer: "A2"},
			ChoiceCard{CardBase: CardBase{Question: "Common Question", Source: "S2"}, RightAnswers: []string{"R3"}, WrongAnswers: []string{"W3"}},
		},
	}

	validFacts, droppedInvalid, droppedDup, issues, err := validateAndSanitizeFacts([]Fact{fact1, fact2}, 2)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if droppedInvalid != 0 || droppedDup != 0 || len(issues) != 0 {
		t.Fatalf("unexpected drops: invalid=%d dup=%d issues=%v", droppedInvalid, droppedDup, issues)
	}
	if len(validFacts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(validFacts))
	}
	if len(validFacts[0].Cards) != 4 || len(validFacts[1].Cards) != 3 {
		t.Fatalf("cards lost: fact1=%d fact2=%d", len(validFacts[0].Cards), len(validFacts[1].Cards))
	}
}

func TestFullDuplicateCardDroppedGracefully(t *testing.T) {
	fact := Fact{
		Name:  "Fact 1",
		Pages: []int{1},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			InputCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			ChoiceCard{CardBase: CardBase{Question: "Choice Q", Source: "S1"}, RightAnswers: []string{"R1"}, WrongAnswers: []string{"W1"}},
			ChoiceCard{CardBase: CardBase{Question: "Choice Q", Source: "S1"}, RightAnswers: []string{"R1"}, WrongAnswers: []string{"W1"}},
		},
	}

	validFacts, droppedInvalid, droppedDup, issues, err := validateAndSanitizeFacts([]Fact{fact}, 1)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if droppedDup != 1 || droppedInvalid != 0 {
		t.Fatalf("expected 1 dropped duplicate, got dup=%d invalid=%d", droppedDup, droppedInvalid)
	}
	if len(validFacts[0].Cards) != 3 {
		t.Fatalf("expected 3 cards after deduplication, got %d", len(validFacts[0].Cards))
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "duplicate card with identical signature") {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestContradictoryBooleanCardsBothDropped(t *testing.T) {
	fact := Fact{
		Name:  "Fact 1",
		Pages: []int{1},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			InputCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			BooleanCard{CardBase: CardBase{Question: "Is Earth flat?", Source: "S1"}, Answer: true},
			BooleanCard{CardBase: CardBase{Question: "Is Earth flat?", Source: "S1"}, Answer: false},
			ChoiceCard{CardBase: CardBase{Question: "Valid Choice", Source: "S1"}, RightAnswers: []string{"R"}, WrongAnswers: []string{"W"}},
		},
	}

	validFacts, droppedInvalid, droppedDup, issues, err := validateAndSanitizeFacts([]Fact{fact}, 1)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if droppedInvalid != 2 || droppedDup != 0 {
		t.Fatalf("expected 2 dropped invalid cards, got invalid=%d dup=%d", droppedInvalid, droppedDup)
	}
	if len(issues) != 2 || !strings.Contains(issues[0], "contradictory boolean card answer") {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(validFacts[0].Cards) != 3 {
		t.Fatalf("expected 3 remaining cards, got %d", len(validFacts[0].Cards))
	}
	for _, c := range validFacts[0].Cards {
		if _, isBool := c.(BooleanCard); isBool {
			t.Fatal("contradictory boolean card was not dropped")
		}
	}
}

func TestInvalidExtraCardDroppedWithoutFailingFact(t *testing.T) {
	fact := Fact{
		Name:      "Fact 1",
		Pages:     []int{1},
		cardPaths: []string{"recall", "recall", "extraCards[0]", "extraCards[1]"},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			InputCard{CardBase: CardBase{Question: "Q1", Source: "S1"}, Answer: "A1"},
			ChoiceCard{CardBase: CardBase{Question: "", Source: "S1"}, RightAnswers: []string{"R"}, WrongAnswers: []string{"W"}},
			ChoiceCard{CardBase: CardBase{Question: "Valid Q", Source: "S1"}, RightAnswers: []string{""}, WrongAnswers: []string{""}},
		},
	}

	validFacts, droppedInvalid, droppedDup, issues, err := validateAndSanitizeFacts([]Fact{fact}, 1)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if droppedInvalid != 2 || droppedDup != 0 {
		t.Fatalf("expected 2 dropped invalid cards, got invalid=%d dup=%d", droppedInvalid, droppedDup)
	}
	if len(validFacts) != 1 || len(validFacts[0].Cards) != 2 {
		t.Fatalf("expected fact to survive with 2 recall cards, got %+v", validFacts)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 logged issues, got %v", issues)
	}
}

func TestEndToEndGeneratorHandlesSanitizationAndAccounting(t *testing.T) {
	f := Fact{
		Name:      "F",
		Pages:     []int{1},
		cardPaths: []string{"recall", "recall", "extraCards[0]", "extraCards[1]"},
		Cards: []Card{
			FlipCard{CardBase: CardBase{Question: "What is SQL?", Source: "SQL Source"}, Answer: "Structured Query Language"},
			InputCard{CardBase: CardBase{Question: "What is SQL?", Source: "SQL Source"}, Answer: "Structured Query Language"},
			// Choice with empty string item - should be sanitized and kept
			ChoiceCard{CardBase: CardBase{Question: "Which keyword filters rows?", Source: "SQL Source"}, RightAnswers: []string{"WHERE", ""}, WrongAnswers: []string{"HAVING", "  "}},
			// Choice with empty question - should be dropped
			ChoiceCard{CardBase: CardBase{Question: "   ", Source: "SQL Source"}, RightAnswers: []string{"R"}, WrongAnswers: []string{"W"}},
		},
	}

	p := stageFixture(f)
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "SQL Content"})
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}
	if r.Stats.DroppedInvalid != 1 {
		t.Fatalf("expected 1 dropped invalid card, got %d", r.Stats.DroppedInvalid)
	}
	if CardCount(r.Facts) != 3 {
		t.Fatalf("expected 3 cards (2 recall + 1 sanitized choice), got %d", CardCount(r.Facts))
	}
	choice := r.Facts[0].Cards[1].(ChoiceCard)
	if !reflect.DeepEqual(choice.RightAnswers, []string{"WHERE"}) || !reflect.DeepEqual(choice.WrongAnswers, []string{"HAVING"}) {
		t.Fatalf("choice was not cleaned properly: %+v", choice)
	}
}
