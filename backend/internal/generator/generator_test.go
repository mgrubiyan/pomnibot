package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// fakeProvider answers by fragment text; nothing goes over the network.
type fakeProvider struct {
	answer func(call int, fragment string, req providers.Request) (string, error)
	delay  time.Duration

	mu       sync.Mutex
	requests []providers.Request
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (f *fakeProvider) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	call := len(f.requests)
	f.mu.Unlock()

	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return providers.Response{}, ctx.Err()
		}
	}

	content, err := f.answer(call, fragmentOf(req.User), req)
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{
		Content: []byte(content),
		Usage:   providers.Usage{PromptTokens: 100, CompletionTokens: 40},
		Model:   "fake-model",
	}, nil
}

func (f *fakeProvider) calls() []providers.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func fragmentOf(user string) string {
	const open, closing = "<фрагмент>\n", "\n</фрагмент>"
	i, j := strings.Index(user, open), strings.LastIndex(user, closing)
	if i < 0 || j < i {
		return ""
	}
	return user[i+len(open) : j]
}

// mc is a card as the fake model writes it.
type mc struct {
	kind        cards.Kind
	q, a, quote string
	topic       string
	distractors []string // factsJSON only
}

func answerJSON(cards ...mc) string {
	items := make([]map[string]string, 0, len(cards))
	for _, c := range cards {
		item := map[string]string{
			"kind":        string(c.kind),
			"question":    c.q,
			"answer":      c.a,
			"explanation": "Так сказано в конспекте. Это прямо следует из цитаты.",
			"quote":       c.quote,
			"topic":       c.topic,
		}
		if c.topic == "" {
			item["topic"] = "Деление клетки"
		}
		items = append(items, item)
	}
	b, err := json.Marshal(map[string]any{"cards": items})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Paragraphs of the test notes. testDoc makes each one a fragment of its own,
// and the fake model tells them apart by the first word.
var paragraphs = []string{
	"Митоз — непрямое деление соматических клеток, при котором из одной материнской клетки образуются две дочерние с одинаковым набором хромосом. Биологическое значение митоза — точная передача наследственной информации.",
	"Мейоз — деление, при котором число хромосом в дочерних клетках уменьшается вдвое. Из одной диплоидной клетки в результате мейоза образуются четыре гаплоидные клетки с половинным набором хромосом.",
	"Цитокинез — разделение цитоплазмы после деления ядра. В животных клетках цитоплазма разделяется перетяжкой, а в растительных клетках в центре клетки формируется клеточная пластинка из пузырьков.",
	"Апоптоз — программируемая гибель клетки. Белок p53 останавливает клеточный цикл при повреждении ДНК и может запустить апоптоз, если повреждение не удаётся исправить за время остановки цикла.",
	"Кроссинговер — обмен участками между гомологичными хромосомами в профазе первого деления мейоза. Он обеспечивает генетическое разнообразие потомства вместе с независимым расхождением хромосом.",
	"Интерфаза — период между делениями, который состоит из пресинтетического, синтетического и постсинтетического периодов. В синтетическом периоде происходит репликация ДНК, и хромосомы удваиваются.",
}

// firstSentence is a quote the fake model can take from a paragraph.
func firstSentence(p string) string {
	return p[:strings.Index(p, ".")+1]
}

// leadingWords is the first n words of p, a quote that differs for each n.
func leadingWords(p string, n int) string {
	return strings.Join(strings.Fields(p)[:n], " ")
}

func firstWord(p string) string {
	return strings.Fields(p)[0]
}

// TestMain silences the generator's logs: tests make it drop cards and
// retry on purpose.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// testDoc joins paragraphs so that each becomes one fragment: the fragment
// size fits the longest paragraph but not any two of them.
func testDoc(t *testing.T, paras ...string) (Document, Options) {
	t.Helper()
	longest, shortest := 0, int(^uint(0)>>1)
	for _, p := range paras {
		n := utf8.RuneCountInString(p)
		longest, shortest = max(longest, n), min(shortest, n)
	}
	if len(paras) > 1 && 2*shortest+2 <= longest {
		t.Fatalf("paragraphs too uneven to be one fragment each: %d..%d", shortest, longest)
	}
	doc := Document{Title: "Лекция 3", Text: strings.Join(paras, "\n\n")}
	return doc, Options{ChunkSize: longest, ChunkOverlap: -1}
}

// checkStats verifies that every card the model returned is accounted for.
func checkStats(t *testing.T, res Result) {
	t.Helper()
	s := res.Stats
	if s.Cards != len(res.Cards) {
		t.Errorf("Stats.Cards = %d, len(Cards) = %d", s.Cards, len(res.Cards))
	}
	accounted := s.DroppedInvalid + s.DroppedQuote + s.DroppedUnsupported + s.DroppedDuplicate + s.DroppedByLimit + s.DroppedVariants + s.Cards
	if s.CardsFromModel != accounted {
		t.Errorf("CardsFromModel = %d, but dropped + returned = %d (%+v)", s.CardsFromModel, accounted, s)
	}
}

func generate(t *testing.T, p providers.Provider, doc Document, opts Options) Result {
	t.Helper()
	res, err := NewGenerator(p, opts).Generate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	checkStats(t, res)
	return res
}

func TestGenerateAcceptsQuoteDifferingInSpacesAndYo(t *testing.T) {
	para := "Ядро окружено «ядерной оболочкой», её наружная мембрана переходит в эндоплазматическую сеть. Внутри ядра находятся хроматин и ядрышко."
	doc, opts := testDoc(t, para)
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return answerJSON(mc{
			kind:  cards.KindFlip,
			q:     "Во что переходит наружная мембрана ядерной оболочки?",
			a:     "В эндоплазматическую сеть.",
			quote: "ядро окружено \"ядерной оболочкой\",  ее наружная\nмембрана переходит в эндоплазматическую сеть",
		}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 {
		t.Fatalf("got %d cards, want 1; stats %+v", len(res.Cards), res.Stats)
	}
	c := res.Cards[0]
	if want := "Ядро окружено «ядерной оболочкой», её наружная мембрана переходит в эндоплазматическую сеть"; c.SourceQuote != want {
		t.Errorf("SourceQuote = %q, want the text of the notes %q", c.SourceQuote, want)
	}
	if c.SourceRef != "Лекция 3, фрагмент 1" {
		t.Errorf("SourceRef = %q", c.SourceRef)
	}
}

func TestGenerateDropsParaphrasedQuote(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return answerJSON(mc{
			kind:  cards.KindFlip,
			q:     "Что такое митоз?",
			a:     "Непрямое деление соматических клеток.",
			quote: "Митоз — это способ непрямого деления соматических клеток организма.",
		}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 0 {
		t.Errorf("got %d cards, want the paraphrased one dropped", len(res.Cards))
	}
	if res.Stats.CardsFromModel != 1 || res.Stats.DroppedQuote != 1 {
		t.Errorf("stats %+v, want 1 from model and 1 dropped by quote", res.Stats)
	}
}

func TestGenerateRetriesInvalidJSON(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(call int, frag string, _ providers.Request) (string, error) {
		if call == 1 {
			return `Вот карточки: {cards: [{"kind": "flip", "question": "Что такое митоз?"`, nil
		}
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое митоз?", a: "Непрямое деление.", quote: firstSentence(frag)}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 {
		t.Fatalf("got %d cards, want 1 after the retry", len(res.Cards))
	}
	s := res.Stats
	if s.ModelCalls != 2 || s.InvalidResponses != 1 || s.ChunksInvalid != 0 {
		t.Errorf("stats %+v, want 2 calls, 1 invalid response, 0 invalid fragments", s)
	}
	calls := p.calls()
	if strings.Contains(calls[0].User, retryNote) || !strings.Contains(calls[1].User, retryNote) {
		t.Error("the retry does not tell the model what went wrong")
	}
}

func TestGenerateSkipsFragmentAfterTwoInvalidAnswers(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0], paragraphs[1])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		if strings.HasPrefix(frag, "Митоз") {
			return "Извините, я не могу составить карточки по этому тексту.", nil
		}
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое мейоз?", a: "Деление с уменьшением числа хромосом.", quote: firstSentence(frag)}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 || !strings.HasPrefix(res.Cards[0].SourceQuote, "Мейоз") {
		t.Fatalf("cards = %+v, want one card from the second fragment", res.Cards)
	}
	if s := res.Stats; s.ChunksInvalid != 1 || s.InvalidResponses != 2 || s.ModelCalls != 3 {
		t.Errorf("stats %+v, want 1 invalid fragment after 2 invalid answers, 3 calls", s)
	}
}

func TestGenerateParsesJSONInMarkdown(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		body := answerJSON(mc{kind: cards.KindFlip, q: "Что такое митоз?", a: "Непрямое деление.", quote: firstSentence(frag)})
		return "Конечно! Вот карточки:\n```json\n" + body + "\n```", nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 || res.Stats.InvalidResponses != 0 {
		t.Errorf("got %d cards and %d invalid answers, want 1 and 0", len(res.Cards), res.Stats.InvalidResponses)
	}
}

func TestGenerateDropsDuplicateQuestions(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0], paragraphs[1])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		q := "Что такое митоз?"
		if strings.HasPrefix(frag, "Мейоз") {
			q = "Что называют митозом"
		}
		return answerJSON(mc{kind: cards.KindFlip, q: q, a: "Непрямое деление.", quote: firstSentence(frag)}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 {
		t.Fatalf("got %d cards, want 1", len(res.Cards))
	}
	if res.Cards[0].SourceRef != "Лекция 3, фрагмент 1" {
		t.Errorf("kept the card from %q, want the first one", res.Cards[0].SourceRef)
	}
	if res.Stats.DroppedDuplicate != 1 {
		t.Errorf("DroppedDuplicate = %d, want 1", res.Stats.DroppedDuplicate)
	}
}

func TestGenerateChoiceWithoutDistractorsBecomesFlip(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		return answerJSON(mc{
			kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "Митоз", quote: firstSentence(frag),
		}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 {
		t.Fatalf("got %d cards, want 1", len(res.Cards))
	}
	if c := res.Cards[0]; c.Kind != cards.KindFlip || c.Options != nil || c.Answer != "Митоз" {
		t.Errorf("card = %+v, want a flip card with the same answer", c)
	}
	if res.Stats.DowngradedToFlip != 1 {
		t.Errorf("DowngradedToFlip = %d, want 1", res.Stats.DowngradedToFlip)
	}
}

func TestGenerateChoiceTakesDistractorsFromOtherFragments(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:5]...)
	var batches [][]cards.Card
	opts.OnCards = func(batch []cards.Card) { batches = append(batches, batch) }
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		word := firstWord(frag)
		if word == "Митоз" {
			return answerJSON(mc{
				kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "Митоз", quote: firstSentence(frag),
			}), nil
		}
		// Other answers come in the model's own spelling: lowercase, with a
		// period. Options must not differ from the right one by form.
		return answerJSON(mc{
			kind: cards.KindInput, q: fmt.Sprintf("Какой термин определён в абзаце номер %d?", len(word)),
			a: strings.ToLower(word) + ".", quote: firstSentence(frag),
		}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 5 {
		t.Fatalf("got %d cards, want 5", len(res.Cards))
	}
	choice := res.Cards[0] // document order, though delivered later
	if choice.Kind != cards.KindChoice || len(choice.Options) != 4 {
		t.Fatalf("first card = %+v, want a choice card with 4 options", choice)
	}
	others := []string{"Мейоз", "Цитокинез", "Апоптоз", "Кроссинговер"}
	for _, o := range choice.Options {
		if o != "Митоз" && !slices.Contains(others, o) {
			t.Errorf("option %q is not an answer from another fragment in the answer's form", o)
		}
	}
	if !slices.Contains(choice.Options, "Митоз") {
		t.Error("options do not include the right answer")
	}
	// The choice card waits for three distractors, from fragments 2, 3 and 4,
	// and goes out with the fourth fragment's cards, not at the very end.
	i := slices.IndexFunc(batches, func(b []cards.Card) bool {
		return slices.ContainsFunc(b, func(c cards.Card) bool { return c.Kind == cards.KindChoice })
	})
	if i < 0 || i == len(batches)-1 || !slices.ContainsFunc(batches[i], func(c cards.Card) bool {
		return c.SourceRef == "Лекция 3, фрагмент 4"
	}) {
		t.Errorf("choice card delivered in batch %d of %d, want with the fourth fragment", i, len(batches))
	}
}

func TestGenerateChoiceTakesDistractorsFromModel(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:2]...)
	var batches [][]cards.Card
	opts.OnCards = func(batch []cards.Card) { batches = append(batches, batch) }
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		if firstWord(frag) != "Митоз" {
			return `{"facts":[]}`, nil
		}
		return factsJSON(fx{quote: firstSentence(frag), cards: []mc{{
			kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "митоз",
			// The answer again, a repeat and an empty one are dropped.
			distractors: []string{"мейоз.", "Амитоз", "МИТОЗ", "Мейоз", " ", "Шизогония"},
		}}}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 {
		t.Fatalf("got %d cards, want 1", len(res.Cards))
	}
	c := res.Cards[0]
	options := slices.Sorted(slices.Values(c.Options))
	if c.Kind != cards.KindChoice || c.Answer != "Митоз" || !slices.Equal(options, []string{"Амитоз", "Мейоз", "Митоз", "Шизогония"}) {
		t.Errorf("card = %+v, want the model's distractors in the answer's form", c)
	}
	// No waiting for other fragments: the card goes out with its own.
	if len(batches) == 0 || len(batches[0]) != 1 || batches[0][0].Kind != cards.KindChoice {
		t.Errorf("batches = %+v, want the choice card in the first one", batches)
	}
}

func TestGenerateChoiceWithFewModelDistractorsUsesPool(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:5]...)
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		word := firstWord(frag)
		if word == "Митоз" {
			return factsJSON(fx{quote: firstSentence(frag), cards: []mc{{
				kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "Митоз",
				distractors: []string{"Митоз", "Амитоз"}, // one left: not enough
			}}}), nil
		}
		return answerJSON(mc{kind: cards.KindInput, q: fmt.Sprintf("Какой термин определён в абзаце номер %d?", len(word)), a: word, quote: firstSentence(frag)}), nil
	}}

	res := generate(t, p, doc, opts)
	i := slices.IndexFunc(res.Cards, func(c cards.Card) bool { return c.Kind == cards.KindChoice })
	if i < 0 || slices.Contains(res.Cards[i].Options, "Амитоз") || len(res.Cards[i].Options) != 4 {
		t.Errorf("cards = %+v, want a choice card with 4 options from the pool", res.Cards)
	}
}

func TestGenerateInputWithLongAnswerBecomesFlip(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		return answerJSON(mc{
			kind: cards.KindInput, q: "Каково биологическое значение?", a: "Точная передача наследственной информации митоза", quote: frag[strings.Index(frag, "Биологическое"):],
		}), nil
	}}
	res := generate(t, p, doc, opts)
	if len(res.Cards) != 1 || res.Cards[0].Kind != cards.KindFlip || res.Stats.InputToFlip != 1 {
		t.Errorf("cards %+v, stats %+v; want one flip card", res.Cards, res.Stats)
	}
}

func TestGenerateDeliversCardsProgressively(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:3]...)
	var batches atomic.Int32
	var sizes []int
	opts.OnCards = func(batch []cards.Card) {
		sizes = append(sizes, len(batch))
		batches.Add(1)
	}
	p := &fakeProvider{answer: func(call int, frag string, _ providers.Request) (string, error) {
		// Fragment N may be answered only after fragment N-1 was delivered:
		// cards collected until the end would never get here.
		deadline := time.Now().Add(2 * time.Second)
		for int(batches.Load()) < call-1 {
			if time.Now().After(deadline) {
				t.Errorf("call %d: cards of the previous fragment were not delivered", call)
				break
			}
			time.Sleep(time.Millisecond)
		}
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое " + strings.ToLower(firstWord(frag)) + "?", a: "Ответ из текста.", quote: firstSentence(frag)}), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 3 || !slices.Equal(sizes, []int{1, 1, 1}) {
		t.Errorf("got %d cards in batches %v, want 3 batches of 1", len(res.Cards), sizes)
	}
	if res.Stats.FirstCardAfter <= 0 || res.Stats.FirstCardAfter > res.Stats.Total {
		t.Errorf("FirstCardAfter = %v, Total = %v", res.Stats.FirstCardAfter, res.Stats.Total)
	}
}

func TestGenerateSurvivesPanickingCallback(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:2]...)
	opts.OnCards = func([]cards.Card) { panic("storage is down") }
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое " + strings.ToLower(firstWord(frag)) + "?", a: "Ответ.", quote: firstSentence(frag)}), nil
	}}
	res := generate(t, p, doc, opts)
	if len(res.Cards) != 2 {
		t.Errorf("got %d cards, want 2 despite the panicking callback", len(res.Cards))
	}
}

func TestGenerateSpreadsCardLimitOverDocument(t *testing.T) {
	doc, opts := testDoc(t, paragraphs...)
	opts.MaxFactsPerDoc = 4
	ordinals := []string{"первое", "второе", "третье"}
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		var list []mc
		for i, o := range ordinals {
			list = append(list, mc{
				kind:  cards.KindFlip,
				q:     fmt.Sprintf("Какое %s свойство у понятия %s?", o, strings.ToLower(firstWord(frag))),
				a:     "Ответ из текста.",
				quote: leadingWords(frag, 5+2*i), // three quotes: three facts
			})
		}
		return answerJSON(list...), nil
	}}

	res := generate(t, p, doc, opts)
	if len(res.Cards) != 4 {
		t.Fatalf("got %d cards, want 4", len(res.Cards))
	}
	// Quotas over 6 fragments are 0,1,2,2,3,4 cards so far: fragments 1 and 4
	// cannot add anything and are not sent at all.
	var refs []string
	for _, c := range res.Cards {
		refs = append(refs, c.SourceRef)
	}
	want := []string{"Лекция 3, фрагмент 2", "Лекция 3, фрагмент 3", "Лекция 3, фрагмент 5", "Лекция 3, фрагмент 6"}
	if !slices.Equal(refs, want) {
		t.Errorf("cards from %v, want %v", refs, want)
	}
	if s := res.Stats; s.ModelCalls != 4 || s.ChunksSkipped != 2 || s.DroppedByLimit != 8 {
		t.Errorf("stats %+v, want 4 calls, 2 skipped fragments, 8 cards over the limit", s)
	}
}

func TestGenerateBackfillsUnusedCardLimit(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:3]...)
	opts.MaxFactsPerDoc = 5
	ordinals := []string{"первое", "второе", "третье"}
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		if strings.HasPrefix(frag, "Цитокинез") {
			return `{"cards":[]}`, nil // the last fragment leaves its share unused
		}
		var list []mc
		for i, o := range ordinals {
			list = append(list, mc{
				kind:  cards.KindFlip,
				q:     fmt.Sprintf("Какое %s свойство у понятия %s?", o, strings.ToLower(firstWord(frag))),
				a:     "Ответ из текста.",
				quote: leadingWords(frag, 5+2*i), // three quotes: three facts
			})
		}
		return answerJSON(list...), nil
	}}

	res := generate(t, p, doc, opts)
	// Quotas 1, 3, 5: fragments 1 and 2 get 1 and 2 cards on the way, the
	// empty third leaves 2 unused, and they go one per fragment from the
	// trimmed cards: 2 + 3 in total, not 5 from the first fragment.
	perFragment := map[string]int{}
	for _, c := range res.Cards {
		perFragment[c.SourceRef]++
	}
	if len(res.Cards) != 5 || perFragment["Лекция 3, фрагмент 1"] != 2 || perFragment["Лекция 3, фрагмент 2"] != 3 {
		t.Errorf("cards per fragment %v, want 2 and 3", perFragment)
	}
	if res.Stats.DroppedByLimit != 1 {
		t.Errorf("DroppedByLimit = %d, want 1", res.Stats.DroppedByLimit)
	}
}

func TestGenerateSurvivesProviderError(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:3]...)
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		if strings.HasPrefix(frag, "Мейоз") {
			return "", errors.New("503 service unavailable")
		}
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое " + strings.ToLower(firstWord(frag)) + "?", a: "Ответ.", quote: firstSentence(frag)}), nil
	}}
	res := generate(t, p, doc, opts)
	if len(res.Cards) != 2 || res.Stats.ChunksFailed != 1 {
		t.Errorf("got %d cards and %d failed fragments, want 2 and 1", len(res.Cards), res.Stats.ChunksFailed)
	}
}

func TestGenerateStopsAfterConsecutiveFailures(t *testing.T) {
	doc, opts := testDoc(t, paragraphs...)
	errDown := errors.New("401 unauthorized")
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return "", errDown }}

	res, err := NewGenerator(p, opts).Generate(context.Background(), doc)
	if !errors.Is(err, errDown) {
		t.Fatalf("Generate() error = %v, want it to wrap the provider error", err)
	}
	if n := len(p.calls()); n != maxConsecutiveFailures {
		t.Errorf("provider called %d times, want %d before giving up", n, maxConsecutiveFailures)
	}
	if res.Stats.ChunksFailed != maxConsecutiveFailures {
		t.Errorf("ChunksFailed = %d", res.Stats.ChunksFailed)
	}
}

func TestGenerateFiltersJunkBeforeModel(t *testing.T) {
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return `{"cards":[]}`, nil }}
	res := generate(t, p, Document{Title: "Клеточный цикл", Text: loadKonspekt(t)}, Options{})

	s := res.Stats
	if s.ChunksFiltered != 3 || s.FilteredBy[junkTitlePage] != 1 || s.FilteredBy[junkTOC] != 1 || s.FilteredBy[junkBibliography] != 1 {
		t.Errorf("filtered %d: %v, want title page, contents and bibliography", s.ChunksFiltered, s.FilteredBy)
	}
	calls := p.calls()
	if len(calls) != s.Chunks-s.ChunksFiltered {
		t.Errorf("%d model calls for %d fragments left after filtering", len(calls), s.Chunks-s.ChunksFiltered)
	}
	for _, req := range calls {
		frag := fragmentOf(req.User)
		for _, junk := range []string{"МИНИСТЕРСТВО", "................", "ISBN"} {
			if strings.Contains(frag, junk) {
				t.Errorf("junk %q reached the model", junk)
			}
		}
		if len(req.Schema) == 0 || req.Temperature != 0 || !strings.Contains(req.System, fmt.Sprintf("Не больше %d фактов", DefaultMaxFactsPerChunk)) {
			t.Error("request lacks the schema, zero temperature or the card limit")
		}
	}
}

func TestGenerateRespectsConcurrency(t *testing.T) {
	for _, concurrency := range []int{1, 3} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			doc, opts := testDoc(t, paragraphs...)
			opts.Concurrency = concurrency
			p := &fakeProvider{
				delay: 20 * time.Millisecond,
				answer: func(_ int, frag string, _ providers.Request) (string, error) {
					return answerJSON(mc{kind: cards.KindFlip, q: "Что такое " + strings.ToLower(firstWord(frag)) + "?", a: "Ответ.", quote: firstSentence(frag)}), nil
				},
			}
			res := generate(t, p, doc, opts)
			if peak := int(p.peak.Load()); peak > concurrency {
				t.Errorf("%d calls at once, limit %d", peak, concurrency)
			}
			if len(res.Cards) != len(paragraphs) {
				t.Fatalf("got %d cards, want %d", len(res.Cards), len(paragraphs))
			}
			for i, c := range res.Cards {
				if want := fmt.Sprintf("Лекция 3, фрагмент %d", i+1); c.SourceRef != want {
					t.Errorf("card %d from %q, want document order", i, c.SourceRef)
				}
			}
		})
	}
}

func TestGenerateStopsOnCancel(t *testing.T) {
	doc, opts := testDoc(t, paragraphs...)
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{answer: func(call int, frag string, _ providers.Request) (string, error) {
		if call == 2 {
			cancel()
		}
		return answerJSON(mc{kind: cards.KindFlip, q: "Что такое " + strings.ToLower(firstWord(frag)) + "?", a: "Ответ.", quote: firstSentence(frag)}), nil
	}}
	res, err := NewGenerator(p, opts).Generate(ctx, doc)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
	if len(p.calls()) > 3 || len(res.Cards) < 1 {
		t.Errorf("%d calls and %d cards after cancel at call 2", len(p.calls()), len(res.Cards))
	}
}

// fx is a fact as the fake model writes it: one quote, cards of several kinds.
type fx struct {
	quote, topic, name string
	cards              []mc
}

func factsJSON(facts ...fx) string {
	items := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		factCards := make([]map[string]any, 0, len(f.cards))
		for _, c := range f.cards {
			card := map[string]any{
				"kind": string(c.kind), "question": c.q, "answer": c.a,
				"explanation": "Так устроен процесс. Это следует из материала.",
			}
			if c.distractors != nil {
				card["distractors"] = c.distractors
			}
			factCards = append(factCards, card)
		}
		topic := f.topic
		if topic == "" {
			topic = "Деление клетки"
		}
		item := map[string]any{"quote": f.quote, "topic": topic, "cards": factCards}
		if f.name != "" {
			item["name"] = f.name
		}
		items = append(items, item)
	}
	b, err := json.Marshal(map[string]any{"facts": items})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func secondSentence(p string) string {
	rest := strings.TrimSpace(p[len(firstSentence(p)):])
	return firstSentence(rest)
}

func TestGenerateFactVariantsShareFactID(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[:5]...)
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		word := firstWord(frag)
		if word != "Митоз" {
			return answerJSON(mc{kind: cards.KindInput, q: fmt.Sprintf("Какой термин определён в абзаце номер %d?", len(word)), a: word, quote: firstSentence(frag)}), nil
		}
		return factsJSON(
			fx{quote: firstSentence(frag), name: "Непрямое деление клеток", cards: []mc{
				{kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "Митоз"},
				{kind: cards.KindInput, q: "Как называется непрямое деление соматических клеток?", a: "митоз"},
				{kind: cards.KindBoolean, q: "Митоз — прямое деление соматических клеток", a: "false"},
				{kind: cards.KindFlip, q: "Что происходит при митозе с набором хромосом?", a: "Дочерние клетки получают такой же набор."},
			}},
			fx{quote: secondSentence(frag), cards: []mc{
				{kind: cards.KindFlip, q: "В чём биологическое значение митоза?", a: "В точной передаче наследственной информации."},
			}},
		), nil
	}}

	res := generate(t, p, doc, opts)
	byFact := map[string][]cards.Card{}
	for _, c := range res.Cards {
		if c.FactID == "" {
			t.Fatalf("card without factId: %+v", c)
		}
		byFact[c.FactID] = append(byFact[c.FactID], c)
	}
	if len(byFact) != 6 || res.Stats.Facts != 6 || res.Stats.FactsFromModel != 6 {
		t.Fatalf("%d facts (stats %d of %d), want 6", len(byFact), res.Stats.Facts, res.Stats.FactsFromModel)
	}
	mitosis := byFact[res.Cards[0].FactID]
	var kinds []cards.Kind
	for _, c := range mitosis {
		kinds = append(kinds, c.Kind)
		if c.FactName != "Непрямое деление клеток" {
			t.Errorf("%s card factName = %q, want the fact's name", c.Kind, c.FactName)
		}
		if c.SourceQuote != strings.TrimSuffix(firstSentence(paragraphs[0]), ".") {
			t.Errorf("%s card quote = %q, want the fact's quote", c.Kind, c.SourceQuote)
		}
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []cards.Kind{cards.KindBoolean, cards.KindChoice, cards.KindFlip, cards.KindInput}) {
		t.Errorf("mitosis fact kinds = %v, want all four", kinds)
	}
	for _, c := range mitosis {
		if c.Kind == cards.KindChoice && len(c.Options) != 4 {
			t.Errorf("choice options = %v, want 4 from other fragments", c.Options)
		}
	}
	// The second fact came without a name: its topic stands in.
	for _, c := range res.Cards {
		if c.FactID != res.Cards[0].FactID && c.FactName != c.Topic {
			t.Errorf("unnamed fact card factName = %q, want topic %q", c.FactName, c.Topic)
		}
	}
}

// A page of dense notes has more than three facts worth a card: by default
// the model is asked for five per fragment, and five are accepted.
func TestGenerateAsksForFiveFactsByDefault(t *testing.T) {
	doc, _ := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		var facts []fx
		for i := range 6 {
			facts = append(facts, fx{quote: leadingWords(frag, 4+i), cards: []mc{{
				kind: cards.KindFlip, q: fmt.Sprintf("Вопрос номер %d о митозе?", i+1), a: "Ответ из текста.",
			}}})
		}
		return factsJSON(facts...), nil
	}}
	res := generate(t, p, doc, Options{})

	req := p.requests[0]
	var schema struct {
		Properties struct {
			Facts struct {
				MaxItems int `json:"maxItems"`
			} `json:"facts"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(req.Schema, &schema); err != nil || schema.Properties.Facts.MaxItems != 5 {
		t.Errorf("schema facts maxItems = %d (err %v), want 5", schema.Properties.Facts.MaxItems, err)
	}
	if !strings.Contains(req.System, "до 5 ключевых фактов") {
		t.Error("system prompt does not ask for up to 5 facts")
	}
	if res.Stats.Facts != 5 {
		t.Errorf("accepted %d facts, want 5 of the 6 returned", res.Stats.Facts)
	}
}

// facts.id is a primary key across all sets: the same notes uploaded twice,
// by one student or two, must not reuse fact ids.
func TestGenerateFactIDsDifferBetweenDocuments(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	factIDOf := func(id string) string {
		p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
			return answerJSON(mc{kind: cards.KindFlip, q: "Что такое митоз?", a: "Деление клеток.", quote: firstSentence(frag)}), nil
		}}
		d := doc
		d.ID = id
		res := generate(t, p, d, opts)
		if len(res.Cards) != 1 || res.Cards[0].FactID == "" {
			t.Fatalf("cards = %+v, want one with a factId", res.Cards)
		}
		return res.Cards[0].FactID
	}

	first, again, other := factIDOf("set-1"), factIDOf("set-1"), factIDOf("set-2")
	if first != again {
		t.Error("the same document got different fact ids")
	}
	if first == other {
		t.Error("two documents with the same text share a fact id")
	}
	if noID, noIDAgain := factIDOf(""), factIDOf(""); noID == noIDAgain {
		t.Error("two documents without an id share a fact id")
	}
}

func TestGenerateFactWithoutQuoteLosesAllCards(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return factsJSON(fx{quote: "Митоз — это способ деления, придуманный природой для роста.", cards: []mc{
			{kind: cards.KindFlip, q: "Что такое митоз?", a: "Деление."},
			{kind: cards.KindBoolean, q: "Митоз — деление клеток", a: "true"},
			{kind: cards.KindInput, q: "Как называется деление соматических клеток?", a: "митоз"},
		}}), nil
	}}
	res := generate(t, p, doc, opts)
	if len(res.Cards) != 0 || res.Stats.DroppedQuote != 3 || res.Stats.FactsFromModel != 1 {
		t.Errorf("cards %d, stats %+v; want every card of the fact dropped by the quote", len(res.Cards), res.Stats)
	}
}

func TestGenerateChoiceWithoutDistractorsDroppedWhenFactHasOtherCards(t *testing.T) {
	doc, opts := testDoc(t, paragraphs[0])
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		return factsJSON(fx{quote: firstSentence(frag), cards: []mc{
			{kind: cards.KindChoice, q: "Как называется непрямое деление соматических клеток?", a: "Митоз"},
			{kind: cards.KindInput, q: "Как называется непрямое деление соматических клеток?", a: "митоз"},
			{kind: cards.KindFlip, q: "Что происходит при митозе с набором хромосом?", a: "Он сохраняется."},
		}}), nil
	}}
	res := generate(t, p, doc, opts)
	var kinds []cards.Kind
	for _, c := range res.Cards {
		kinds = append(kinds, c.Kind)
	}
	// No other fragments, no distractors: the choice card goes, the fact
	// stays covered by input and flip. Sharing a question with the input
	// card does not make either a duplicate.
	if !slices.Equal(kinds, []cards.Kind{cards.KindInput, cards.KindFlip}) {
		t.Errorf("kinds = %v, want [input flip]", kinds)
	}
	if s := res.Stats; s.DroppedVariants != 1 || s.DroppedDuplicate != 0 || s.DowngradedToFlip != 0 {
		t.Errorf("stats %+v, want 1 dropped variant, no duplicates, no downgrade", s)
	}
}

func TestGenerateSameFactTwiceIsDuplicate(t *testing.T) {
	shared := "Клеточный цикл регулируется циклинами и циклинзависимыми киназами."
	doc, opts := testDoc(t, paragraphs[0]+" "+shared, paragraphs[1]+" "+shared)
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return factsJSON(fx{quote: shared, cards: []mc{
			{kind: cards.KindFlip, q: "Чем регулируется клеточный цикл?", a: "Циклинами и киназами."},
			{kind: cards.KindBoolean, q: "Клеточный цикл регулируется циклинами", a: "true"},
		}}), nil
	}}
	res := generate(t, p, doc, opts)
	if len(res.Cards) != 2 || res.Stats.Facts != 1 || res.Stats.DroppedDuplicate != 2 {
		t.Errorf("cards %d, stats %+v; want the second copy of the fact dropped", len(res.Cards), res.Stats)
	}
}

func TestGenerateDropsUnsupportedAnswers(t *testing.T) {
	para := "Клеточный цикл — это период жизни клетки от одного деления до следующего или до её гибели. " +
		"Клеточный цикл состоит из интерфазы и собственно деления."
	doc, opts := testDoc(t, para)
	p := &fakeProvider{answer: func(_ int, frag string, _ providers.Request) (string, error) {
		return factsJSON(fx{quote: frag, cards: []mc{
			// Seen live: a verbatim quote and the wrong term, taken from the question.
			{kind: cards.KindChoice, q: "Как называется период жизни клетки от одного деления до следующего, состоящий из интерфазы и собственно деления?", a: "Интерфаза"},
			// Seen live: the kind's name written as the answer.
			{kind: cards.KindInput, q: "Как называется период жизни клетки между делениями?", a: "input"},
			{kind: cards.KindInput, q: "Как называется период жизни клетки от одного деления до следующего?", a: "клеточный цикл"},
			{kind: cards.KindFlip, q: "Из чего состоит клеточный цикл?", a: "Из интерфазы и деления."},
		}}), nil
	}}
	res := generate(t, p, doc, opts)
	var answers []string
	for _, c := range res.Cards {
		answers = append(answers, c.Answer)
	}
	if !slices.Equal(answers, []string{"клеточный цикл", "Из интерфазы и деления."}) {
		t.Errorf("answers = %q, want the two supported cards", answers)
	}
	if res.Stats.DroppedUnsupported != 2 {
		t.Errorf("DroppedUnsupported = %d, want 2", res.Stats.DroppedUnsupported)
	}
}
