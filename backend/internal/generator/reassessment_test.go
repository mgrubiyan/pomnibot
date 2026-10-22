package generator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestSourceTargetRejectionSurvivesPipeline(t *testing.T) {
	quote := "Время поиска индексного блока равно log2 B."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: quote}}}
	var topic Topic
	if err := json.Unmarshal([]byte(`{"id":"t","title":"Поиск","blockIds":["a"],"objectives":[{"id":"o","text":"Время поиска блока","blockIds":["a"]}]}`), &topic); err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:REVIEW") {
			return `{"verdicts":[{"index":0,"accept":false,"defect":"unsupported_answer","problem":"Question or answer adds an unsupported claim","evidence":[{"blockId":"a","quote":"` + quote + `"}],"covered":["o"]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Как оценивается время поиска записи?", a: "log2 B", quote: quote}), nil
	}}
	r := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "d", Structure: &d}, topic, nil)
	if r.err != nil || len(r.Cards) != 0 {
		t.Fatalf("object of source operation changed: %+v %v", r.Cards, r.err)
	}
}

func TestUnsupportedConditionRejectionSurvivesPipeline(t *testing.T) {
	quote := "Инструмент используется через значительный интервал времени работы базы."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: quote}}}
	p := &fakeProvider{answer: func(_ int, _ string, req providers.Request) (string, error) {
		if strings.HasPrefix(req.System, "STAGE:REVIEW") {
			return `{"verdicts":[{"index":0,"accept":false,"defect":"unsupported_answer","problem":"Question or answer adds an unsupported claim","evidence":[{"blockId":"a","quote":"` + quote + `"}],"covered":["o"]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Через какой временной интервал используется инструмент?", a: "Когда накоплена достаточная нагрузка за значительный интервал времени работы базы", quote: quote}), nil
	}}
	topic := Topic{ID: "t", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", Text: "Timing", BlockIDs: []string{"a"}}}}
	r := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "d", Structure: &d}, topic, nil)
	err := r.err
	if err != nil || len(r.Cards) != 0 {
		t.Fatalf("invented sufficiency condition survived: %+v %v", r.Cards, err)
	}
}

func TestGroupingCanRepairAnUnjustifiedSplit(t *testing.T) {
	parts := []Plan{{Topics: []Topic{
		{ID: "normal", Title: "Insert with space", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "Insert with space", BlockIDs: []string{"a"}}}},
		{ID: "overflow", Title: "Insert with overflow", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "Insert with overflow", BlockIDs: []string{"b"}}}},
		{ID: "search", Title: "Search", BlockIDs: []string{"c"}, Objectives: []Objective{{ID: "oc", Text: "Search", BlockIDs: []string{"c"}}}},
	}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["normal","overflow"],"reason":"Two cases of insertion in the same index"},{"topicIds":["search"],"reason":"Different operation"}]}`, nil
		}
		return `{"groups":[["normal"],["overflow"],["search"]]}`, nil
	}}
	got, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(got.Topics) != 2 || len(got.Topics[0].Objectives) != 2 || got.Topics[0].Objectives[0].ID != "oa" || got.Topics[0].Objectives[1].ID != "ob" || got.Topics[1].Objectives[0].ID != "oc" {
		t.Fatalf("split retained or objectives lost: %+v %v", got, err)
	}
}

func TestReviewSchemaRequiresOneVerdictPerCard(t *testing.T) {
	var shape struct {
		Properties struct {
			Verdicts struct {
				Min int `json:"minItems"`
				Max int `json:"maxItems"`
			} `json:"verdicts"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(reviewSchema(4), &shape); err != nil {
		t.Fatal(err)
	}
	if shape.Properties.Verdicts.Min != 4 || shape.Properties.Verdicts.Max != 4 {
		t.Fatal("review cardinality is unconstrained")
	}
}
