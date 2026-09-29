package grader

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestLocal(t *testing.T) {
	tests := []struct {
		expected, given string
		want            localVerdict
	}{
		// Form of the answer does not matter.
		{"Анафаза", "анафаза.", localMatch},
		{"ёж", "Еж", localMatch},
		{"на отсортированном массиве", "отсортированный массив", localMatch},
		{"циклины и циклинзависимые киназы", "циклинзависимые киназы, циклины", localMatch},
		{"Потомственное дворянство", "  потомственное   дворянство!", localMatch},
		// A typo per six letters or so.
		{"анафаза", "онафаза", localMatch},
		{"кроссинговер", "кросинговер", localMatch},
		{"гены-супрессоры опухолей", "гены супрессоры опухоли", localMatch},
		// Code and formulas are compared as written, spaces aside.
		{"cos(180° − α) = −cos α", "cos (180°-α)=-cos α", localMatch},
		{"O(n log n)", "o(nlogn)", localMatch},
		// Numbers must match: a date is either right or wrong.
		{"1689", "1698", localMismatch},
		{"27 июня 1709", "27 июня 1710", localMismatch},
		{"около 90 %", "90%", localMatch},
		{"", "", localMismatch},
		{"анафаза", "", localMismatch},
		// Other words: let the model judge.
		{"анафаза", "метафаза", localUnsure},
		{"возрастает", "не возрастает", localUnsure},
		{"на отсортированном массиве", "массив должен быть упорядочен", localUnsure},
		{"cos(180° − α) = −cos α", "cos(180° − α) = cos α", localUnsure},
	}
	for _, tt := range tests {
		if got := local(tt.expected, tt.given); got != tt.want {
			t.Errorf("local(%q, %q) = %v, want %v", tt.expected, tt.given, got, tt.want)
		}
	}
}

type fakeModel struct {
	answer string
	err    error
	block  bool

	mu   sync.Mutex
	reqs []providers.Request
}

func (m *fakeModel) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	if m.block {
		<-ctx.Done()
		return providers.Response{}, ctx.Err()
	}
	if m.err != nil {
		return providers.Response{}, m.err
	}
	return providers.Response{Content: []byte(m.answer)}, nil
}

var sorted = Input{
	Question: "На каком массиве применим бинарный поиск?",
	Expected: "на отсортированном массиве",
	Quote:    "Бинарный поиск работает только на отсортированном массиве",
	Given:    "массив должен быть упорядочен",
}

func TestCheckAsksModelWhenUnsure(t *testing.T) {
	model := &fakeModel{answer: "```json\n{\"correct\": true, \"reason\": \"Упорядоченный — то же, что отсортированный.\"}\n```"}
	got := (&Grader{Model: model}).Check(context.Background(), sorted)
	if !got.Correct || got.Method != MethodModel || got.Reason == "" {
		t.Errorf("verdict %+v, want correct by the model with a reason", got)
	}
	if len(model.reqs) != 1 {
		t.Fatalf("model called %d times, want once", len(model.reqs))
	}
	req := model.reqs[0]
	for _, part := range []string{sorted.Question, sorted.Expected, sorted.Quote, sorted.Given} {
		if !strings.Contains(req.User, part) {
			t.Errorf("request lacks %q:\n%s", part, req.User)
		}
	}
	if len(req.Schema) == 0 || req.Temperature != 0 || req.System == "" {
		t.Errorf("request %+v, want a system prompt, a schema and temperature 0", req)
	}
}

func TestCheckLeavesModelOutWhenLocalDecides(t *testing.T) {
	model := &fakeModel{answer: `{"correct": true, "reason": ""}`}
	g := &Grader{Model: model}
	for _, in := range []Input{
		{Expected: "Анафаза", Given: "анафазы"},
		{Expected: "1689", Given: "1698"},
	} {
		got := g.Check(context.Background(), in)
		if got.Method != MethodLocal || got.Correct != (in.Given == "анафазы") {
			t.Errorf("%q for %q: verdict %+v", in.Given, in.Expected, got)
		}
	}
	if len(model.reqs) != 0 {
		t.Errorf("model called %d times, want never", len(model.reqs))
	}
}

// When the model cannot answer, the local check has the last word: the
// student waits no longer than Timeout and sees a verdict anyway.
func TestCheckFallsBackWhenModelFails(t *testing.T) {
	for name, model := range map[string]*fakeModel{
		"error":      {err: errors.New("unavailable")},
		"no json":    {answer: "Ответ студента верный."},
		"no verdict": {answer: `{"reason": "Не знаю."}`},
		"timeout":    {block: true},
	} {
		began := time.Now()
		got := (&Grader{Model: model, Timeout: 50 * time.Millisecond}).Check(context.Background(), sorted)
		if got.Correct || got.Method != MethodFallback {
			t.Errorf("%s: verdict %+v, want wrong by fallback", name, got)
		}
		if time.Since(began) > time.Second {
			t.Errorf("%s: took %v, want the timeout kept", name, time.Since(began))
		}
	}
}

func TestCheckWithoutModel(t *testing.T) {
	var g *Grader
	if got := g.Check(context.Background(), sorted); got.Correct || got.Method != MethodLocal {
		t.Errorf("nil grader: verdict %+v, want wrong by the local check", got)
	}
	if got := g.Check(context.Background(), Input{Expected: "Анафаза", Given: "анафаза"}); !got.Correct {
		t.Errorf("nil grader: verdict %+v, want the exact answer right", got)
	}
}
