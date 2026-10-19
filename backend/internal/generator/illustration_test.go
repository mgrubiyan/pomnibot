package generator

import (
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// codeFragment is a slide of example code as normalization leaves it: the
// indented lines are glued to the line above.
const codeFragment = `Наследование. Пример
class A: def hello(self): return "A"
class B(A): pass
class C(B): def hello(self): return "C"
obj = C()
print(obj.hello())
Схема: A → B → C
class Dog(Animal): def speak(self): return "Гав"
Car extends Vehicle, Shape → Circle.
Наследование позволяет классу-потомку использовать методы родителя.`

func TestExampleNames(t *testing.T) {
	got := exampleNames(codeFragment, "")
	for _, name := range []string{"A", "B", "C", "obj", "Dog", "Car"} {
		if !got.has(name) {
			t.Errorf("%q not found among %v", name, got)
		}
	}
	// A base class or a method may be the subject of a lecture: Exception,
	// Animal of a zoo, __init__ and speak are not invented for one example.
	for _, name := range []string{"Animal", "Vehicle", "hello", "speak", "self", "print", "pass", "Shape", "Circle"} {
		if got.has(name) {
			t.Errorf("%q taken for an example name", name)
		}
	}
}

func TestExampleNamesSkipConcepts(t *testing.T) {
	text := "Singleton — порождающий паттерн, который гарантирует единственный экземпляр.\nclass Singleton: pass\nclass Builder: pass\nclass Config(Builder): pass"
	got := exampleNames(text, "Паттерн Builder")
	for _, name := range []string{"Singleton", "Builder"} {
		if got.has(name) {
			t.Errorf("%q is a concept the notes define or the title names, not an example name", name)
		}
	}
	if !got.has("Config") {
		t.Errorf("Config not found among %v", got)
	}
}

// Formulas are not code: F = m · a and y = f(x) name no example, and an
// arrow between words is no scheme of classes.
func TestExampleNamesSkipFormulas(t *testing.T) {
	got := exampleNames("Второй закон Ньютона: F = m · a.\nФункция y = f(x) задана на отрезке. Fe → Fe2O3 при окислении. H2O → H2 + O2. Путь курса: Python → Java → Kotlin.", "")
	if len(got) != 0 {
		t.Errorf("names = %v, want none", got)
	}
}

func TestAboutExample(t *testing.T) {
	names := exampleNames(codeFragment, "")
	for _, tc := range []struct {
		name string
		card modelCard
		want bool
	}{
		{"which class a class inherits from", modelCard{Kind: cards.KindChoice, Question: "От какого класса наследуется класс B?", Answer: "A"}, true},
		{"what example code prints", modelCard{Kind: cards.KindChoice, Question: "Что выведет print(obj.hello())?", Answer: "C"}, true},
		{"an example class as the answer", modelCard{Kind: cards.KindInput, Question: "Какой класс переопределяет метод hello?", Answer: "C"}, true},
		{"a lowercase letter of the example", modelCard{Kind: cards.KindFlip, Question: "Какой класс является родителем класса c?", Answer: "B"}, false},
		{"a false claim about the example", modelCard{Kind: cards.KindBoolean, Question: "Наследование позволяет использовать методы родителя.", Answer: cards.AnswerTrue, Alt: "Класс Dog не может переопределить метод родителя."}, true},
		{"an example name inside a word", modelCard{Kind: cards.KindFlip, Question: "Что такое ABI и API?", Answer: "Интерфейсы"}, false},
		{"C++ is not class C", modelCard{Kind: cards.KindFlip, Question: "Есть ли множественное наследование в C++?", Answer: "Да"}, false},
		{"the concept the example shows", modelCard{Kind: cards.KindChoice, Question: "Что позволяет классу-потомку использовать методы родителя?", Answer: "Наследование"}, false},
		{"a base class is no example name", modelCard{Kind: cards.KindFlip, Question: "Что делает метод speak у Animal?", Answer: "Возвращает звук"}, false},
		{"a scheme restated as a claim", modelCard{Kind: cards.KindBoolean, Question: "Схема наследования: A → B → C.", Answer: cards.AnswerTrue}, true},
		{"the example shown in the question", modelCard{Kind: cards.KindChoice, Question: "От какого класса наследуется Dog в class Dog(Animal)?", Answer: "Animal"}, false},
		{"an English article is no class A", modelCard{Kind: cards.KindFlip, Question: "A class may extend one class. Which keyword is used?", Answer: "extends"}, false},
		{"an English question about an example class", modelCard{Kind: cards.KindFlip, Question: "Which class does Dog extend?", Answer: "Animal"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := aboutExample(tc.card, names); got != tc.want {
				t.Errorf("aboutExample(%q, %q) = %v, want %v", tc.card.Question, tc.card.Answer, got, tc.want)
			}
		})
	}
}

func TestExampleNamesLowercaseLetterAfterClass(t *testing.T) {
	got := exampleNames("Class a -> Class b: класс b наследуется от a.", "")
	if !got.has("a") || !got.has("b") {
		t.Errorf("names = %v, want a and b", got)
	}
	c := modelCard{Kind: cards.KindChoice, Question: "От какого класса наследуется класс b?", Answer: "a"}
	if !aboutExample(c, got) {
		t.Errorf("aboutExample(%q) = false, want true", c.Question)
	}
	if len(got) != 2 {
		t.Errorf("names = %v, want a and b only", got)
	}
}

// A card on the names of example code goes; a card on a keyword of the
// language the code uses stays.
func TestGenerateDropsCardsAboutExampleNames(t *testing.T) {
	text := "Наследование позволяет создать класс на основе существующего. В Java наследование задаётся ключевым словом extends.\nclass Animal { }\nclass Dog extends Animal { }\nЗдесь класс Dog наследуется от класса Animal."
	doc := Document{Title: "Лекция 4. Наследование", Text: text}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return factsJSON(
			fx{quote: "Здесь класс Dog наследуется от класса Animal.", cards: []mc{
				{kind: cards.KindInput, q: "От какого класса наследуется класс Dog?", a: "Animal"},
				{kind: cards.KindBoolean, q: "Класс Dog наследуется от класса Animal.", a: cards.AnswerTrue, distractors: []string{"Класс Animal наследуется от класса Dog."}},
			}},
			fx{quote: "В Java наследование задаётся ключевым словом extends.", cards: []mc{
				{kind: cards.KindInput, q: "Каким ключевым словом в Java задаётся наследование?", a: "extends"},
			}},
		), nil
	}}

	res := generate(t, p, doc, Options{})
	if len(res.Cards) != 1 || res.Cards[0].Answer != "extends" {
		t.Errorf("cards = %+v, want the extends card alone", res.Cards)
	}
	if res.Stats.DroppedExample != 2 {
		t.Errorf("DroppedExample = %d, want 2", res.Stats.DroppedExample)
	}
}
