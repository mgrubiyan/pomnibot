package generator

import (
	"context"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"reflect"
	"testing"
)

func requiredFormatsFixture() Fact {
	cs := []Card{
		InputCard{CardBase: CardBase{Question: "Recall"}, Answer: "A and B"},
		TableCard{CardBase: CardBase{Question: "Sort"}, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "Y", Rows: []string{"B"}}}},
		ChoiceCard{CardBase: CardBase{Question: "Choose"}, RightAnswers: []string{"A", "B"}, WrongAnswers: []string{"C"}},
		BooleanCard{CardBase: CardBase{Question: "True?"}, Answer: false},
		FlipCard{CardBase: CardBase{Question: "Explain"}, Answer: "A and B"},
	}
	return Fact{Name: "F", Cards: cs}
}
func TestGenerationKeepsAllFormatsWithMandatoryPair(t *testing.T) {
	p := stageFixture([]KnowledgeGoal{learningGoal("g1", "F")}, []goalCards{{GoalID: "g1", Content: requiredFormatsFixture()}})
	batches := 0
	r, e := NewGenerator(p, Options{OnBatch: func(Batch) { batches++ }}).Generate(context.Background(), Document{Text: "A and B"})
	if e != nil || batches != 1 || len(r.Cards) != 5 {
		t.Fatalf("%+v %v", r, e)
	}
	for i, kind := range []cards.Kind{cards.KindFlip, cards.KindBoolean, cards.KindChoice, cards.KindTable, cards.KindInput} {
		if r.Cards[i].Kind != kind || r.Cards[i].FactID != r.Facts[0].ID {
			t.Fatal("lost card format or shared progress")
		}
	}
	c := r.Cards[2]
	if !reflect.DeepEqual(c.Options, []string{"A", "B", "C"}) || !cards.MatchChoiceAnswer(c.Answer, `["B","A"]`) || cards.MatchChoiceAnswer(c.Answer, `["A"]`) {
		t.Fatal("lost correct answer set")
	}
	if !reflect.DeepEqual(r.Cards[3].Table, &cards.Table{Columns: []string{"X", "Y"}, Items: []cards.TableItem{{Text: "A", Column: "X"}, {Text: "B", Column: "Y"}}}) {
		t.Fatal("lost table mappings")
	}
}
func TestInvalidCardsRejectWholeGeneration(t *testing.T) {
	for _, defect := range []string{"missing flip", "missing input", "blank answer", "duplicate question"} {
		t.Run(defect, func(t *testing.T) {
			f := requiredFormatsFixture()
			switch defect {
			case "missing flip":
				f.Cards = f.Cards[:4]
			case "missing input":
				f.Cards = f.Cards[1:]
			case "blank answer":
				f.Cards[0] = InputCard{CardBase{"Recall"}, " "}
			case "duplicate question":
				f.Cards = append(f.Cards, f.Cards[0])
			}
			r, e := NewGenerator(stageFixture([]KnowledgeGoal{learningGoal("g1", "F")}, []goalCards{{GoalID: "g1", Content: f}}), Options{OnBatch: func(Batch) { t.Fatal("published invalid set") }}).Generate(context.Background(), Document{Text: "A and B"})
			if e == nil || len(r.Cards) != 0 {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
