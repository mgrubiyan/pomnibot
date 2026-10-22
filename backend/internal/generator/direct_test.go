package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestDirectDocumentGroundsEachCardAndDeliversOneBatch(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Page: 1, Text: "Индекс ускоряет\nпоиск записей."}, {ID: "b", Page: 2, Text: "Ключ определяет поиск."}}}
	payload := `{"cards":[{"name":"Назначение","topic":"Индекс","question":"Для чего служит индекс?","answer":"Ускоряет поиск записей.","evidence":[{"blockId":"a","quote":"Индекс ускоряет поиск записей."}]},{"name":"Ключ","topic":"Индекс","question":"Что определяет поиск?","answer":"Ключ.","evidence":[{"blockId":"missing","quote":"Ключ определяет поиск."}]}]}`
	calls, delivered := 0, 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		if !strings.Contains(r.User, "Ключ определяет поиск.") {
			t.Fatal("distant source missing")
		}
		return payload, nil
	}}
	r, err := NewGenerator(p, Options{OnBatch: func(b Batch) { delivered += len(b.Cards) }}).Generate(context.Background(), Document{ID: "d", Title: "Лекция", Structure: &d})
	if err != nil || calls != 1 || delivered != 1 || len(r.Cards) != 1 || r.Stats.DroppedQuote != 1 || r.Stats.CardsFromModel != 2 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if !strings.HasPrefix(d.Blocks[0].Text, r.Cards[0].SourceQuote) || r.Cards[0].Explanation != r.Cards[0].SourceQuote || r.Cards[0].SourceRef != "Лекция, стр. 1" || len(r.Facts) != 1 {
		t.Fatalf("lost source/save fields: %+v", r)
	}
	if r.Stats.FirstCardAfter <= 0 || r.Stats.Total <= 0 {
		t.Fatal("missing timing")
	}
}

func TestDirectFailureNeverRetriesOrPublishes(t *testing.T) {
	for _, payload := range []string{`{`, `{"cards":null}`, `{"facts":[]}`} {
		calls := 0
		p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { calls++; return payload, nil }}
		d := sourceFixture()
		r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published malformed response") }}).Generate(context.Background(), Document{Structure: &d})
		if err == nil || calls != 1 || r.Stats.Total <= 0 || r.Stats.InvalidResponses != 1 {
			t.Fatalf("failure metrics missing: %+v %v", r, err)
		}
	}
}

func TestDirectRejectsInvalidAndDuplicateCardsWithoutLosingGoodOnes(t *testing.T) {
	d := sourceFixture()
	card := directCard{Name: "Удаление", Topic: "Индекс", Question: "Когда индекс не перестраивается при удалении записи?", Answer: "Если занято не более 900 байт.", Evidence: []directEvidence{{BlockID: "s", Quote: d.Blocks[1].Text}}}
	invalid := card
	invalid.Answer = ""
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		b, _ := json.Marshal(directResponse{Cards: []directCard{card, invalid, card}})
		return string(b), nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Structure: &d})
	if err != nil || len(r.Cards) != 1 || r.Stats.DroppedInvalid != 1 || r.Stats.DroppedDuplicate != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDirectTransportErrorKeepsUsageAndTiming(t *testing.T) {
	d := sourceFixture()
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Usage: providers.Usage{PromptTokens: 10, CostUSD: .001}}, errors.New("outage")
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Structure: &d})
	if err == nil || r.Stats.PromptTokens != 10 || r.Stats.CostUSD != .001 || r.Stats.Total <= 0 || r.Stats.ChunksFailed != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDirectWholeSourceLimitBeforeBilling(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "s", Text: strings.Repeat("x", 59000)}}}
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		if !strings.Contains(r.User, d.Blocks[0].Text) {
			t.Fatal("source cut")
		}
		return `{"cards":[]}`, nil
	}}
	_, err := NewGenerator(p, Options{SourceBudget: providers.OpenRouterProfile().SourceBytes}).Generate(context.Background(), Document{Structure: &d})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	_, err = NewGenerator(p, Options{SourceBudget: 10}).Generate(context.Background(), Document{Structure: &d})
	if !errors.Is(err, providers.ErrCapability) || calls != 1 {
		t.Fatalf("billed oversized source: calls=%d err=%v", calls, err)
	}
}

func TestPlainTextUsesTheSameSingleDocumentRequest(t *testing.T) {
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		if r.System != directPrompt || !strings.Contains(r.User, "Индекс ускоряет поиск.") {
			t.Fatal("plain text took a different pipeline")
		}
		return `{"cards":[]}`, nil
	}}
	_, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "Индекс ускоряет поиск."})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestQuotesCannotSkipSourceConditions(t *testing.T) {
	source := "Полный индекс хранит записи в отсортированном порядке. Только при положительном значении параметра. Неполный индекс хранит номера блоков основного файла."
	c := directCard{Name: "Индекс", Topic: "Хранение", Question: "Какие индексы существуют?", Answer: "Полный и неполный.", Evidence: []directEvidence{{BlockID: "s", Quote: "Полный индекс хранит записи в отсортированном порядке. ... Неполный индекс хранит номера блоков основного файла."}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		data, _ := json.Marshal(directResponse{Cards: []directCard{c}})
		return string(data), nil
	}}
	d := material.Document{Blocks: []material.Block{{ID: "s", Text: source}}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Structure: &d})
	if err != nil || len(r.Cards) != 0 || r.Stats.DroppedQuote != 1 {
		t.Fatalf("accepted discontinuous quote: %+v %v", r, err)
	}
}

// Opt-in, offline validation of a paid recording. Source and schema must match;
// the old output is deliberately rechecked under the new quote validator.
func TestRecordedResponseValidation(t *testing.T) {
	dir := os.Getenv("POMNIBOT_DIRECT_RECORDING")
	if dir == "" {
		t.Skip("private recording required; no live API calls")
	}
	data, err := os.ReadFile(filepath.Join(dir, "e2.json.requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var trace struct {
		Request  providers.Request
		Response providers.Response
	}
	if err = json.Unmarshal(bytes.TrimSpace(data), &trace); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct{ Ingest ingest.Result }
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	var title struct {
		Title string `json:"title"`
	}
	if err = json.Unmarshal([]byte(trace.Request.User), &title); err != nil {
		t.Fatal(err)
	}
	p := providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
		if r.User != trace.Request.User || !bytes.Equal(r.Schema, trace.Request.Schema) {
			t.Fatal("source or schema changed")
		}
		response := trace.Response
		response.Cached = true
		response.Usage = providers.Usage{}
		return response, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Title: title.Title, Structure: &input.Ingest.Document})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Cards) != 23 || r.Stats.DroppedQuote != 1 {
		t.Fatalf("unexpected filtering: %d cards, %d quotes", len(r.Cards), r.Stats.DroppedQuote)
	}
	// The only removed card must be the independently audited bad classification;
	// all 18 independently usable cards, including the bulleted update rule, stay.
	for _, c := range r.Cards {
		if strings.HasPrefix(c.Question, "Какие два метода организации") {
			t.Fatal("bad classification retained")
		}
	}
	t.Log("old recording: one bad card rejected, zero good cards lost, 23 retained")
}

func TestSourceQuotePreservesMeaningAndOriginalFormatting(t *testing.T) {
	for _, tc := range []struct {
		source, quote string
		ok            bool
	}{
		{"Правило:\n• Индекс не перестраивается.\n• Читается один блок.", "Индекс не перестраивается. Читается один блок.", true},
		{"Индекс не перестраивается.", "Индекс перестраивается.", false},
		{"Число записей не более 900.", "Число записей менее 900.", false},
		{"x ≤ 0", "x < 0", false},
		{"x = −1", "x = 1", false},
		{"Индекс хранит ключ.", "", false},
	} {
		quote, ok := sourceQuote(tc.source, tc.quote)
		if ok != tc.ok {
			t.Fatalf("source=%q quote=%q accepted=%v", tc.source, tc.quote, ok)
		}
		if ok && !strings.Contains(tc.source, quote) {
			t.Fatal("returned a rewritten source")
		}
	}
}

func TestPlainTextRetainsPageReferences(t *testing.T) {
	text := "Первая страница.\nВторая страница."
	p := &fakeProvider{answer: func(_ int, _ string, _ providers.Request) (string, error) {
		return `{"cards":[{"name":"Страница","topic":"Текст","question":"Какая страница?","answer":"Вторая.","evidence":[{"blockId":"p2","quote":"Вторая страница."}]}]}`, nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Title: "Файл", Text: text, PageStarts: []int{0, len("Первая страница.\n")}})
	if err != nil || len(r.Cards) != 1 || r.Cards[0].SourceRef != "Файл, стр. 2" {
		t.Fatalf("lost pages: %+v %v", r, err)
	}
}
