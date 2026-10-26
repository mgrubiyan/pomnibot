package generator

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const recallJSON = `{"needsSvg":true,"question":"Что такое x²?\nПример 🧠","answer":"  x × x\nα  ","source":" Формула\nα ≠ β ","sourceQuote":"«α»\n\t"}`

func recallReply(extras string) string {
	return `{"figures":[],"issues":[],"facts":[{"name":"F","pages":[1],"recall":` + recallJSON + `,"extraCards":[` + extras + `]}]}`
}

func TestRecallExpansionAndAccounting(t *testing.T) {
	for _, extra := range []string{"", `{"kind":"recall","question":"Second?","answer":"B","source":"S","sourceQuote":""},{"kind":"recall","question":"Third?","answer":"C","source":"T","sourceQuote":"  цитата\n"}`} {
		calls, batches := 0, 0
		raw := recallReply(extra)
		p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
			calls++
			return providers.Response{Content: []byte(raw)}, nil
		})
		r, err := NewGenerator(p, Options{OnBatch: func(Batch) { batches++ }}).Generate(t.Context(), Document{Text: "source"})
		if err != nil {
			t.Fatal(err)
		}
		pairs := 1
		if extra != "" {
			pairs = 3
		}
		if calls != 2 || batches != 1 || len(r.Facts[0].Cards) != pairs*2 {
			t.Fatalf("calls=%d batches=%d result=%+v", calls, batches, r)
		}
		for i := 0; i < pairs; i++ {
			flip := r.Facts[0].Cards[i].(FlipCard)
			input := r.Facts[0].Cards[pairs+i].(InputCard)
			if !reflect.DeepEqual(flip.CardBase, input.CardBase) || flip.Answer != input.Answer {
				t.Fatal("pair differs")
			}
		}
		if r.Facts[0].Cards[0].Base().Source != " Формула\nα ≠ β " || r.Facts[0].Cards[0].(FlipCard).Answer != "  x × x\nα  " {
			t.Fatal("changed bytes")
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var saved Result
		if err = json.Unmarshal(b, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Artifact.Response != raw || CardCount(saved.Artifact.Facts) != pairs*2 || !strings.Contains(string(b), `\"recall\"`) {
			t.Fatal("lost artifact")
		}
		var stats map[string]any
		b, _ = json.Marshal(r.Stats)
		_ = json.Unmarshal(b, &stats)
		if stats["cardsSynthesized"] != float64(pairs*2) || r.Stats.Cards != pairs*2 || r.Stats.CardsFromModel != 0 {
			t.Fatalf("%s", b)
		}
	}
}

func TestMixedRecallExtrasPreserveContentOrderAndCounts(t *testing.T) {
	raw := recallReply(`{"kind":"table","question":"Sort","source":"Table source","sourceQuote":"T","columns":[{"name":"X","rows":["A"]},{"name":"Y","rows":["B"]}]},{"kind":"recall","question":"Second?","answer":"B","source":"S","sourceQuote":"  цитата\n"},{"kind":"choice","question":"Choose","source":"Choice source","sourceQuote":"C","rightAnswers":["A","B"],"wrongAnswers":["C"]},{"kind":"boolean","question":"True?","source":"Boolean source","sourceQuote":"","answer":false},{"kind":"recall","question":"Third?","answer":"C","source":"T","sourceQuote":""}`)
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(raw)}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.Cards != 9 || r.Stats.CardsFromModel != 3 || r.Stats.CardsSynthesized != 6 {
		t.Fatalf("%+v", r.Stats)
	}
	cs := r.Facts[0].Cards
	for i, want := range []int{0, 0, 0, 1, 2, 3, 4, 4, 4} {
		if cardDifficulty(cs[i]) != want {
			t.Fatalf("order at %d: %T", i, cs[i])
		}
	}
	for i, want := range []string{"Что такое x²?\nПример 🧠", "Second?", "Third?"} {
		flip, input := cs[i].(FlipCard), cs[6+i].(InputCard)
		if flip.Question != want || !reflect.DeepEqual(flip.CardBase, input.CardBase) || flip.Answer != input.Answer {
			t.Fatalf("pair %d differs", i)
		}
	}
	if cs[3].(BooleanCard).Answer || cs[4].(ChoiceCard).RightAnswers[1] != "B" || cs[5].(TableCard).Columns[1].Rows[0] != "B" {
		t.Fatal("lost extra content")
	}
}

func TestRecallContractRejectsMalformedFieldsAtomically(t *testing.T) {
	for _, section := range []string{"fact", "recall", "extraRecall"} {
		fields := []string{"question", "answer", "source", "sourceQuote"}
		if section == "fact" {
			fields = []string{"name", "pages", "recall", "extraCards"}
		}
		if section == "extraRecall" {
			fields = append(fields, "kind")
		}
		for _, field := range fields {
			for _, defect := range []string{"missing", "null", "wrong type"} {
				t.Run(section+"/"+field+"/"+defect, func(t *testing.T) {
					var root map[string]any
					_ = json.Unmarshal([]byte(recallReply("")), &root)
					fact := root["facts"].([]any)[0].(map[string]any)
					target := fact
					path := "facts[0]"
					switch section {
					case "recall":
						target = fact["recall"].(map[string]any)
						path += ".recall"
					case "extraRecall":
						target = map[string]any{"kind": "recall", "question": "Other?", "answer": "A", "source": "S", "sourceQuote": ""}
						fact["extraCards"] = []any{target}
						path += ".extraCards[0]"
					}
					switch defect {
					case "missing":
						delete(target, field)
					case "null":
						target[field] = nil
					case "wrong type":
						target[field] = true
					}
					raw, _ := json.Marshal(root)
					calls := 0
					p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
						calls++
						return providers.Response{Content: raw}, nil
					})
					r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published invalid response") }}).Generate(t.Context(), Document{Text: "source"})
					if err == nil || calls != 1 || len(r.Facts) != 0 || len(r.Artifact.Facts) != 0 {
						t.Fatalf("%+v %v", r, err)
					}
					if fields := diagnosticFields(t, err); fields["path"] != path+"."+field {
						t.Fatalf("%v", fields)
					}
				})
			}
		}
	}
}

func TestRecallContractRejectsLegacyKindsUnknownFieldsAndDuplicates(t *testing.T) {
	extra := `{"kind":"recall","question":"Other?","answer":"A","source":"S","sourceQuote":""}`
	for name, raw := range map[string]string{
		"legacy cards":         `{"figures":[],"issues":[],"facts":[{"name":"F","pages":[1],"cards":[{"kind":"flip","question":"Q","answer":"A","source":"S","sourceQuote":""},{"kind":"input","question":"Q","answer":"A","source":"S","sourceQuote":""}]}]}`,
		"extra flip":           recallReply(strings.Replace(extra, `"recall"`, `"flip"`, 1)),
		"extra input":          recallReply(strings.Replace(extra, `"recall"`, `"input"`, 1)),
		"unknown recall field": strings.Replace(recallReply(""), `"recall":{`, `"recall":{"other":1,`, 1),
		"primary kind":         strings.Replace(recallReply(""), `"recall":{`, `"recall":{"kind":"recall",`, 1),
		"unknown extra field":  recallReply(strings.Replace(extra, `"kind":`, `"other":1,"kind":`, 1)),
		"duplicate primary":    recallReply(`{"kind":"recall",` + recallJSON[1:]),
		"duplicate extras":     recallReply(extra + "," + extra),
		"bad pages":            strings.Replace(recallReply(extra), `"pages":[1]`, `"pages":[2]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{Content: []byte(raw)}, nil
			})
			r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published invalid response") }}).Generate(t.Context(), Document{Text: "source"})
			if err == nil || len(r.Facts) != 0 {
				t.Fatalf("%+v %v", r, err)
			}
			if name == "duplicate primary" || name == "duplicate extras" {
				want := "facts[0].extraCards[0].question"
				if name == "duplicate extras" {
					want = "facts[0].extraCards[1].question"
				}
				if fields := diagnosticFields(t, err); fields["path"] != want {
					t.Fatalf("%v", fields)
				}
			}
		})
	}
}

func TestLegacyReportsKeepIndependentCardsAndZeroSynthesizedCount(t *testing.T) {
	raw := `{"facts":[{"name":"Old","pages":[1],"cards":[{"kind":"flip","question":"Q","answer":"A","source":"S","sourceQuote":"quote"},{"kind":"input","question":"Other?","answer":"B","source":"T","sourceQuote":""}]}],"stats":{"cards":2,"cardsFromModel":2},"artifact":{"version":"direct-generation-v11","response":"legacy model response"}}`
	var r Result
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Stats.CardsSynthesized != 0 || r.Stats.CardsFromModel != 2 || r.Facts[0].Cards[1].(InputCard).Answer != "B" || r.Artifact.Response != "legacy model response" {
		t.Fatalf("%+v", r)
	}
}
