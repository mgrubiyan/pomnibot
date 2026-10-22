package generator

import (
	"context"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestIndependentGoalsShareTopicButNotProgress(t *testing.T) {
	const payload = `{"response":{"facts":[{"name":"Очередь","cards":[{"kind":"boolean","question":"Ожидает ли проверки документ в очереди?","answer":true},{"kind":"flip","question":"Что такое очередь?","answer":"Документы, ожидающие проверки."},{"kind":"input","question":"Что такое очередь?","answer":"Документы, ожидающие проверки."}]},{"name":"Архив","cards":[{"kind":"flip","question":"Что такое архив?","answer":"Документы с завершённой проверкой."},{"kind":"input","question":"Что такое архив?","answer":"Документы с завершённой проверкой."}]}]},"grounding":[{"factIndex":0,"kind":"flip","question":"Что такое очередь?","topic":"Разделы","evidence":[{"blockId":"text","quote":"Очередь — документы, ожидающие проверки."}]},{"factIndex":0,"kind":"boolean","question":"Ожидает ли проверки документ в очереди?","topic":"Разделы","evidence":[{"blockId":"text","quote":"Очередь — документы, ожидающие проверки."}]},{"factIndex":1,"kind":"flip","question":"Что такое архив?","topic":"Разделы","evidence":[{"blockId":"text","quote":"Архив — документы с завершённой проверкой."}]},{"factIndex":0,"kind":"input","question":"Что такое очередь?","topic":"Разделы","evidence":[{"blockId":"text","quote":"Очередь — документы, ожидающие проверки."}]},{"factIndex":1,"kind":"input","question":"Что такое архив?","topic":"Разделы","evidence":[{"blockId":"text","quote":"Архив — документы с завершённой проверкой."}]}]}`
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return payload, nil }}
	result, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{ID: "goals", Text: "Очередь — документы, ожидающие проверки. Архив — документы с завершённой проверкой."})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 2 || len(result.Cards) != 5 {
		t.Fatalf("lost learning content: %+v", result)
	}
	if result.Facts[0].Topic != result.Facts[1].Topic || result.Facts[0].ID == result.Facts[1].ID {
		t.Fatal("independent goals must share a topic without sharing progress")
	}
	if result.Cards[0].FactID != result.Cards[1].FactID || result.Cards[0].FactID != result.Cards[2].FactID || result.Cards[0].FactID == result.Cards[3].FactID || result.Cards[3].FactID != result.Cards[4].FactID {
		t.Fatal("variants must share only their own goal's progress")
	}
	if result.Cards[0].Question != "Что такое очередь?" || result.Cards[3].Question != "Что такое архив?" {
		t.Fatal("initial selection must see each goal's flip")
	}
}
