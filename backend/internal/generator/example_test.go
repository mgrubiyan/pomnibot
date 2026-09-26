package generator_test

import (
	"context"
	"fmt"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// cannedModel stands in for gigachat.New: it answers every fragment with one
// fact checked in two forms.
type cannedModel struct{}

func (cannedModel) Complete(context.Context, providers.Request) (providers.Response, error) {
	return providers.Response{Content: []byte(`{"facts":[{
		"quote": "Анафаза — самая короткая фаза митоза",
		"topic": "Фазы митоза",
		"cards": [
			{"kind":"input","question":"Какая фаза митоза самая короткая?","answer":"Анафаза","explanation":"Так сказано в конспекте."},
			{"kind":"boolean","question":"Метафаза — самая короткая фаза митоза.","answer":"false","explanation":"Самая короткая — анафаза."}
		]}]}`)}, nil
}

func ExampleNewGenerator() {
	gen := generator.NewGenerator(cannedModel{}, generator.Options{
		// Called as fragments are done, in document order: the feed can show
		// the first cards before the whole document is processed.
		OnCards: func(batch []cards.Card) { fmt.Println("saved", len(batch), "cards") },
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
	for _, c := range res.Cards {
		fmt.Printf("[%s] %s → %s (%s)\n", c.Kind, c.Question, c.Answer, c.SourceRef)
	}
	// Output:
	// saved 2 cards
	// [input] Какая фаза митоза самая короткая? → Анафаза (Лекция 3, фрагмент 1)
	// [boolean] Метафаза — самая короткая фаза митоза. → false (Лекция 3, фрагмент 1)
}
