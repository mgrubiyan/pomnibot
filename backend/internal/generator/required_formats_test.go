package generator

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func requiredFormatsFixture() directResponse {
	// Deliberately unordered: decoding must retain every format and sort by kind.
	cs := []Card{
		InputCard{CardBase: CardBase{Question: "Recall"}, Answer: "A and B"},
		TableCard{CardBase: CardBase{Question: "Sort"}, Columns: []Column{{Name: "X", Rows: []string{"A"}}, {Name: "Y", Rows: []string{"B"}}}},
		ChoiceCard{CardBase: CardBase{Question: "Choose"}, RightAnswers: []string{"A", "B"}, WrongAnswers: []string{"C"}},
		BooleanCard{CardBase: CardBase{Question: "True?"}, Answer: false},
		FlipCard{CardBase: CardBase{Question: "Explain"}, Answer: "A and B"},
	}
	r := directResponse{Response: AIResponse{Facts: []Fact{{Name: "F", Cards: cs}}}}
	for _, content := range cs {
		c, _ := storedCard(content)
		r.Grounding = append(r.Grounding, grounding{FactIndex: 0, Kind: c.Kind, Question: c.Question, Topic: "T", Evidence: []directEvidence{{BlockID: "text", Quote: "A and B"}}})
	}
	return r
}

func TestFactRequiresFlipAndInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing cards.Kind
	}{
		{name: "missing flip", missing: cards.KindFlip},
		{name: "missing input", missing: cards.KindInput},
		{name: "both required and all optional kinds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := requiredFormatsFixture().Response
			var kept []Card
			for _, content := range r.Facts[0].Cards {
				c, _ := storedCard(content)
				if c.Kind != tc.missing {
					kept = append(kept, content)
				}
			}
			r.Facts[0].Cards = kept
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var decoded AIResponse
			err = json.Unmarshal(b, &decoded)
			if tc.missing != "" {
				if err == nil || !strings.Contains(err.Error(), string(tc.missing)) {
					t.Fatalf("missing %s: got %v", tc.missing, err)
				}
				return
			}
			if err != nil || len(decoded.Facts[0].Cards) != 5 {
				t.Fatalf("lost valid formats: %+v, %v", decoded, err)
			}
		})
	}
}

func TestGenerationRejectsMissingMandatoryKindsWithoutRepair(t *testing.T) {
	for _, missing := range []cards.Kind{cards.KindFlip, cards.KindInput} {
		t.Run(string(missing), func(t *testing.T) {
			r := requiredFormatsFixture()
			var kept []Card
			for _, content := range r.Response.Facts[0].Cards {
				c, _ := storedCard(content)
				if c.Kind != missing {
					kept = append(kept, content)
				}
			}
			r.Response.Facts[0].Cards = kept
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { calls++; return string(b), nil }}
			result, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published incomplete fact") }}).Generate(context.Background(), Document{Text: "A and B"})
			if err == nil || calls != 1 || result.Stats.InvalidResponses != 1 || len(result.Facts) != 0 || len(result.Cards) != 0 {
				t.Fatalf("missing %s: %+v, %v, calls=%d", missing, result, err, calls)
			}
		})
	}
}

func TestGenerationKeepsAllFormatsWithMandatoryPair(t *testing.T) {
	b, err := json.Marshal(requiredFormatsFixture())
	if err != nil {
		t.Fatal(err)
	}
	calls, batches := 0, 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { calls++; return string(b), nil }}
	result, err := NewGenerator(p, Options{OnBatch: func(b Batch) {
		batches++
		if len(b.Facts) != 1 || len(b.Cards) != 5 {
			t.Fatalf("incomplete batch: %+v", b)
		}
	}}).Generate(context.Background(), Document{ID: "all-kinds", Text: "A and B"})
	if err != nil || calls != 1 || batches != 1 || len(result.Facts) != 1 || len(result.Cards) != 5 {
		t.Fatalf("%+v, %v, calls=%d batches=%d", result, err, calls, batches)
	}
	for i, kind := range []cards.Kind{cards.KindFlip, cards.KindBoolean, cards.KindChoice, cards.KindTable, cards.KindInput} {
		c := result.Cards[i]
		if c.Kind != kind || c.FactID != result.Facts[0].ID || result.Facts[0].Topic != "T" || len(c.Evidence) != 1 || c.SourceQuote != "A and B" {
			t.Fatalf("lost order, grouping or grounding: %+v", result)
		}
	}
	choice := result.Cards[2]
	if !reflect.DeepEqual(choice.Options, []string{"A", "B", "C"}) || !reflect.DeepEqual(choice.RightAnswers, []string{"A", "B"}) ||
		!cards.MatchChoiceAnswer(choice.Answer, `["B","A"]`) || cards.MatchChoiceAnswer(choice.Answer, `["A"]`) || cards.MatchChoiceAnswer(choice.Answer, `["A","B","C"]`) {
		t.Fatalf("choice must retain the exact correct set: %+v", choice)
	}
	wantTable := &cards.Table{Columns: []string{"X", "Y"}, Items: []cards.TableItem{{Text: "A", Column: "X"}, {Text: "B", Column: "Y"}}}
	if !reflect.DeepEqual(result.Cards[3].Table, wantTable) {
		t.Fatalf("lost table relationships: %+v", result.Cards[3].Table)
	}
}

func TestGenerationDropsWholeFactWhenMandatoryCardFailsValidation(t *testing.T) {
	for _, kind := range []cards.Kind{cards.KindFlip, cards.KindInput} {
		for _, defect := range []string{"blank answer", "missing grounding", "duplicate grounding", "bad quote", "different topic"} {
			t.Run(string(kind)+"/"+defect, func(t *testing.T) {
				r := requiredFormatsFixture()
				// A later, complete fact reuses the same questions. Rejected facts
				// must not reserve questions used by later complete facts.
				good := requiredFormatsFixture()
				good.Response.Facts[0].Name = "Complete"
				r.Response.Facts = append(r.Response.Facts, good.Response.Facts...)
				for _, ref := range good.Grounding {
					ref.FactIndex = 1
					r.Grounding = append(r.Grounding, ref)
				}
				for i, ref := range r.Grounding {
					if ref.FactIndex != 0 || ref.Kind != kind {
						continue
					}
					switch defect {
					case "blank answer":
						for j, content := range r.Response.Facts[0].Cards {
							switch c := content.(type) {
							case FlipCard:
								if kind == cards.KindFlip {
									c.Answer = " "
									r.Response.Facts[0].Cards[j] = c
								}
							case InputCard:
								if kind == cards.KindInput {
									c.Answer = " "
									r.Response.Facts[0].Cards[j] = c
								}
							}
						}
					case "missing grounding":
						r.Grounding = append(r.Grounding[:i], r.Grounding[i+1:]...)
					case "duplicate grounding":
						r.Grounding = append(r.Grounding, ref)
					case "bad quote":
						r.Grounding[i].Evidence = []directEvidence{{BlockID: "text", Quote: "invented"}}
					case "different topic":
						r.Grounding[i].Topic = "Other"
					}
					break
				}
				b, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				calls, batches := 0, 0
				p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { calls++; return string(b), nil }}
				result, err := NewGenerator(p, Options{OnBatch: func(b Batch) {
					batches++
					if len(b.Facts) != 1 || b.Facts[0].Name != "Complete" || len(b.Cards) != 5 {
						t.Fatalf("published partial fact: %+v", b)
					}
				}}).Generate(context.Background(), Document{Text: "A and B"})
				if err != nil || calls != 1 || batches != 1 || len(result.Facts) != 1 || result.Facts[0].Name != "Complete" || len(result.Cards) != 5 || result.Stats.CardsFromModel != 10 || result.Stats.DroppedInvalid+result.Stats.DroppedQuote != 5 {
					t.Fatalf("non-atomic filtering: %+v, %v, calls=%d batches=%d", result, err, calls, batches)
				}
			})
		}
	}
}

func TestGenerationDropsFactWhenInputDuplicatesAnAcceptedFact(t *testing.T) {
	r := requiredFormatsFixture()
	// Only input collides with the accepted fact: a valid new flip alone
	// must not allow the rest of the second fact to be published.
	r.Response.Facts = append(r.Response.Facts, Fact{Name: "Another goal", Cards: []Card{
		FlipCard{CardBase: CardBase{Question: "Another question"}, Answer: "A and B"},
		InputCard{CardBase: CardBase{Question: "Recall"}, Answer: "A and B"},
	}})
	r.Grounding = append(r.Grounding,
		grounding{FactIndex: 1, Kind: cards.KindFlip, Question: "Another question", Topic: "T", Evidence: []directEvidence{{BlockID: "text", Quote: "A and B"}}},
		grounding{FactIndex: 1, Kind: cards.KindInput, Question: "Recall", Topic: "T", Evidence: []directEvidence{{BlockID: "text", Quote: "A and B"}}},
	)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { calls++; return string(b), nil }}
	result, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "A and B"})
	if err != nil || calls != 1 || len(result.Facts) != 1 || len(result.Cards) != 5 || result.Stats.DroppedDuplicate != 1 || result.Stats.DroppedInvalid != 1 {
		t.Fatalf("published fact without a unique input: %+v, %v, calls=%d", result, err, calls)
	}
}

func TestInputAnswersPreserveConditionsWithExistingLocalGrader(t *testing.T) {
	// These are answer shapes recommended by the generation prompt, exercised
	// without a model: full conditions, unordered lists and canonical formulas.
	for _, tc := range []struct {
		question, expected, given, incomplete string
	}{
		{"При каких условиях выполняется правило?", "Если ключ уникален и значение не пустое", "если значение не пустое и ключ уникален", "Если ключ уникален"},
		{"Назовите три части модели.", "Структурная, целостная и манипуляционная", "манипуляционная, структурная, целостная", "структурная и целостная"},
		{"Запишите формулу времени t через a и b.", "t = a + b", "t=a+b", "t=a"},
	} {
		g := &grader.Grader{}
		in := grader.Input{Question: tc.question, Expected: tc.expected, Given: tc.given}
		if v := g.Check(context.Background(), in); !v.Correct || v.Method != grader.MethodLocal {
			t.Fatalf("full answer rejected locally: %+v: %+v", in, v)
		}
		in.Given = tc.incomplete
		if v := g.Check(context.Background(), in); v.Correct || v.Method != grader.MethodLocal {
			t.Fatalf("incomplete answer accepted locally: %+v: %+v", in, v)
		}
	}
}
