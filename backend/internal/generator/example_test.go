package generator_test

import (
	"context"
	"fmt"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// cannedModel stands in for gigachat.New: it answers every fragment with one
// fact checked in two forms.
type cannedModel struct{}

func (cannedModel) Complete(context.Context, providers.Request) (providers.Response, error) {
	return providers.Response{Content: []byte(`{"facts":[{
		"quote": "Анафаза — самая короткая фаза митоза",
		"topic": "Фазы митоза",
		"name": "Самая короткая фаза митоза",
		"cards": [
			{"kind":"input","question":"Какая фаза митоза самая короткая?","answer":"Анафаза","explanation":"Так сказано в конспекте."},
			{"kind":"boolean","question":"Метафаза — самая короткая фаза митоза.","answer":"false","explanation":"Самая короткая — анафаза."}
		]}]}`)}, nil
}

func ExampleNewGenerator() {
	gen := generator.NewGenerator(cannedModel{}, generator.Options{
		// Called as fragments are done, in document order: the feed can show
		// the first cards before the whole document is processed.
		// A fact comes in the batch with its first card, to be stored first.
		OnBatch: func(b generator.Batch) { fmt.Println("saved", len(b.Facts), "fact and", len(b.Cards), "cards") },
	})
	res, err := gen.Generate(context.Background(), generator.Document{
		Title: "Лекция 3",
		Text: "Митоз — непрямое деление соматических клеток. Анафаза — самая короткая фаза митоза: " +
			"сестринские хроматиды расходятся к противоположным полюсам клетки.",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s (%s)\n", res.Facts[0].Name, res.Facts[0].Topic)
	for _, c := range res.Cards {
		fmt.Printf("  [%s] %s → %s\n", c.Kind, c.Question, c.Answer)
	}
	// Output:
	// saved 1 fact and 2 cards
	// Самая короткая фаза митоза (Фазы митоза)
	//   [input] Какая фаза митоза самая короткая? → Анафаза
	//   [boolean] Метафаза — самая короткая фаза митоза. → false
}
