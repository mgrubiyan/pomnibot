package generator

import (
	"context"

	"reflect"
	"testing"
)

func requiredFormatsFixture() Fact {
	cs := []Card{
		InputCard{CardBase: CardBase{Source: "source", Question: "Explain"}, Answer: "A and B"},
		TableCard{CardBase: CardBase{Source: "source", Question: "Sort"}, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "Y", Rows: []string{"B"}}}},
		ChoiceCard{CardBase: CardBase{Source: "source", Question: "Choose"}, RightAnswers: []string{"A", "B"}, WrongAnswers: []string{"C"}},
		BooleanCard{CardBase: CardBase{Source: "source", Question: "True?"}, Answer: false},
		FlipCard{CardBase: CardBase{Source: "source", Question: "Explain"}, Answer: "A and B"},
	}
	return Fact{Name: "F", Pages: []int{1}, Cards: cs}
}
func TestGenerationKeepsAllFormatsWithMandatoryPair(t *testing.T) {
	p := stageFixture(requiredFormatsFixture())
	batches := 0
	r, e := NewGenerator(p, Options{OnBatch: func(Batch) { batches++ }}).Generate(context.Background(), Document{Text: "A and B"})
	if e != nil || batches != 1 || CardCount(r.Facts) != 5 {
		t.Fatalf("%+v %v", r, e)
	}
	cs := r.Facts[0].Cards
	if _, ok := cs[0].(FlipCard); !ok {
		t.Fatal("lost flip order")
	}
	if _, ok := cs[1].(BooleanCard); !ok {
		t.Fatal("lost boolean order")
	}
	if !reflect.DeepEqual(cs[2].(ChoiceCard).RightAnswers, []string{"A", "B"}) || cs[3].(TableCard).Columns[1].Name != "Y" {
		t.Fatal("lost choice or table")
	}
	if _, ok := cs[4].(InputCard); !ok {
		t.Fatal("lost input order")
	}

}
func TestInvalidCardsRejectWholeGeneration(t *testing.T) {
	for _, defect := range []string{"missing recall", "blank answer", "duplicate question"} {
		t.Run(defect, func(t *testing.T) {
			f := requiredFormatsFixture()
			switch defect {
			case "missing recall":
				f.Cards = f.Cards[:4]
			case "blank answer":
				f.Cards[4] = FlipCard{cardBase("Recall"), " "}
			case "duplicate question":
				f.Cards = append(f.Cards, f.Cards[4])
			}
			r, e := NewGenerator(stageFixture(f), Options{OnBatch: func(Batch) { t.Fatal("published invalid set") }}).Generate(context.Background(), Document{Text: "A and B"})
			if e == nil || CardCount(r.Facts) != 0 {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
