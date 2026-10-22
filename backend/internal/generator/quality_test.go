package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestSplitTasksFitBudgetAndRetainRequiredContext(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "definition", Kind: "text", Text: strings.Repeat("definition ", 20)},
		{ID: "condition", Kind: "text", Text: strings.Repeat("condition ", 20)},
		{ID: "table", Kind: "table", Cells: [][]string{{"key", "address"}}},
		{ID: "example", Kind: "code", Text: strings.Repeat("example ", 20)},
		{ID: "unrelated", Kind: "code", Text: strings.Repeat("other ", 200)},
	}}
	topic := Topic{ID: "t", BlockIDs: []string{"definition", "condition", "table", "example"}, RelatedBlockIDs: []string{"unrelated"}, Objectives: []Objective{
		{ID: "rule", BlockIDs: []string{"definition", "condition", "table"}},
		{ID: "apply", BlockIDs: []string{"definition", "condition", "example"}},
	}}
	tasks := splitTopicTasks(Plan{Topics: []Topic{topic}}, d, 900)
	seen := map[string]bool{}
	for _, task := range tasks {
		if _, err := topicSource(task, d, 900); err != nil {
			t.Error(err)
		}
		for _, o := range task.Objectives {
			seen[o.ID] = true
			for _, id := range o.BlockIDs {
				if !strings.Contains(strings.Join(task.BlockIDs, ","), id) {
					t.Errorf("lost required block %s", id)
				}
			}
		}
	}
	if len(seen) != 2 {
		t.Fatal("lost objectives")
	}
}

func TestPlanningRetriesUnassignedContent(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Definition."}, {ID: "b", Text: "Necessary condition."}}}
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, _ providers.Request) (string, error) {
		calls++
		if calls == 1 {
			return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"o","text":"Definition","blockIds":["a"]}]}]}`, nil
		}
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["a","b"],"objectives":[{"id":"o","text":"Definition","blockIds":["a","b"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(plan.Topics[0].BlockIDs) != 2 {
		t.Fatalf("content silently lost: calls=%d plan=%+v", calls, plan)
	}
}

func TestRejectedReviewReceivesOneIndependentRecheck(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "An index speeds up search."}}}
	topic := Topic{ID: "t", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", BlockIDs: []string{"a"}}}}
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		accept := calls > 1
		if accept && !strings.Contains(r.User, "priorVerdicts") {
			t.Error("recheck lacks feedback")
		}
		problem := "Answer is in the source, so it is a hint."
		if accept {
			problem = ""
		}
		b, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: accept, Problem: problem, Evidence: []material.Evidence{{BlockID: "a", Quote: d.Blocks[0].Text}}, Covered: []string{"o"}}}})
		return string(b), nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Question: "What is the purpose of an index?"}}, nil)
	if err != nil || len(vs) != 1 || !vs[0].Accept || calls != 2 {
		t.Fatalf("false rejection retained: calls=%d vs=%+v err=%v", calls, vs, err)
	}
}

func TestRepairSchedulerSplitsAllTopicsAndBoundsFutileAttempts(t *testing.T) {
	d := material.Document{}
	p := Plan{}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		d.Blocks = append(d.Blocks, material.Block{ID: id, Text: "A complete rule."})
		p.Topics = append(p.Topics, Topic{ID: id, BlockIDs: []string{id}, Objectives: []Objective{{ID: id, BlockIDs: []string{id}}}})
	}
	attempts := map[string]int{}
	for round := 0; round < 2; round++ {
		tasks := nextRepairTasks(p, map[string]bool{}, d, 12, 1000, attempts)
		if len(tasks) != 5 {
			t.Fatalf("round %d lost topics: %d", round, len(tasks))
		}
	}
	if tasks := nextRepairTasks(p, map[string]bool{}, d, 12, 1000, attempts); len(tasks) != 0 {
		t.Fatal("repeats without bound")
	}
}

func TestReviewPixelRegionUsesActualReceivedImageDimensions(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1200, 800))); err != nil {
		t.Fatal(err)
	}
	d := material.Document{Pages: []material.Page{{Number: 1, Image: buf.Bytes()}}, Blocks: []material.Block{{ID: "a", Page: 1, Kind: "diagram"}}}
	e := material.Evidence{BlockID: "a", Kind: "image", Region: &material.Region{120, 80, 600, 400}, Observation: "An arrow connects A and B."}
	got, ok := resolveEvidence(d, e)
	if !ok || got.Region == nil || *got.Region != (material.Region{.1, .1, .5, .5}) {
		t.Fatalf("valid region lost: %+v %v", got, ok)
	}
	e.Region = &material.Region{120, 80, 1300, 400}
	if _, ok := resolveEvidence(d, e); ok {
		t.Fatal("out-of-image region accepted")
	}
}

func TestAssignedSourceTakesPrecedenceOverModelExclusion(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "A complete rule."}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"o","text":"Rule","blockIds":["a"]}]}],"excluded":[{"blockId":"a","kind":"service","reason":"Page decoration"}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || len(plan.Excluded) != 0 || len(plan.Topics) != 1 || len(p.requests) != 1 {
		t.Fatalf("assigned source discarded: %+v %v", plan, err)
	}
}

func TestPlanningIncludesKnownObjectiveDependenciesInTopic(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Definition."}, {ID: "b", Text: "Necessary condition."}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"o","text":"Rule","blockIds":["a","b"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || len(plan.Topics) != 1 || len(plan.Topics[0].BlockIDs) != 2 || len(p.requests) != 1 {
		t.Fatalf("valid dependencies lost: %+v %v", plan, err)
	}
}

func TestRepairsDoNotCopyCoveredObjectivesBackIntoEachPacket(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "done", Kind: "text", Text: strings.Repeat("covered ", 300)}, {ID: "missing", Text: "Necessary definition and condition."}}}
	topic := Topic{ID: "t", BlockIDs: []string{"done", "missing"}, Objectives: []Objective{{ID: "a", BlockIDs: []string{"done"}}, {ID: "b", BlockIDs: []string{"missing"}}}}
	tasks := nextRepairTasks(Plan{Topics: []Topic{topic}}, map[string]bool{"a": true}, d, 12, 500, map[string]int{})
	if len(tasks) != 1 {
		t.Fatal("lost repair")
	}
	source, err := topicSource(tasks[0], d, 500)
	if err != nil || strings.Contains(source, "covered") || !strings.Contains(source, "condition") {
		t.Fatalf("unrelated covered material reintroduced: %s %v", source, err)
	}
}

func TestScopeBindingRetainsPageTitleAboveSmallerSubheading(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "title", Page: 1, Kind: "heading", Text: "Плотный индекс", Region: &material.Region{.2, .03, .8, .11}},
		{ID: "subtitle", Page: 1, Kind: "heading", Text: "Алгоритм поиска данных", Region: &material.Region{.2, .12, .8, .17}},
		{ID: "rule", Page: 1, Kind: "text", Text: "Бинарный поиск."},
	}}
	p := Plan{Topics: []Topic{{ID: "t", Title: "Алгоритм поиска", BlockIDs: []string{"rule"}, Objectives: []Objective{{ID: "o", Text: "Поиск", Scope: []string{"плотный индекс"}, BlockIDs: []string{"rule"}}}}}}
	bindObjectiveScopes(&p, d)
	if err := validatePlan(p, d); err != nil {
		t.Fatal(err)
	}
}

func TestRecheckCannotApproveInventedEvidenceOrLoopOnRejection(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "An index speeds up search."}}}
	topic := Topic{BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", BlockIDs: []string{"a"}}}}
	for _, invented := range []bool{false, true} {
		calls := 0
		p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
			calls++
			v := verdict{Index: 0, Problem: "Missing necessary condition.", Evidence: []material.Evidence{{BlockID: "a", Quote: d.Blocks[0].Text}}}
			if calls > 1 && invented {
				v.Accept = true
				v.Problem = ""
				v.Evidence[0].Quote = "Invented evidence."
			}
			raw, _ := json.Marshal(map[string]any{"verdicts": []verdict{v}})
			return string(raw), nil
		}}
		vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Question: "A question?"}}, nil)
		if err != nil || len(vs) != 1 || vs[0].Accept || calls != 2 {
			t.Fatalf("unsafe/unbounded recheck: %+v err=%v calls=%d", vs, err, calls)
		}
	}
}

func TestPlanningRetryCannotRemoveAnOriginalDifficultGoal(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "A difficult condition."}, {ID: "b", Text: "An example."}}}
	calls := 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		calls++
		if calls == 1 {
			return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"hard","text":"Difficult condition","blockIds":["a"]}]}]}`, nil
		}
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["a","b"],"objectives":[{"id":"easy","text":"Example","blockIds":["b"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || plan.Topics[0].Objectives[0].ID != "hard" || len(unassignedBlocks(plan, d)) != 0 || len(plan.Topics[0].Objectives) != 2 {
		t.Fatalf("hard goal silently removed: %+v %v", plan, err)
	}
}

func TestSplitTasksAccountsForSourceImagesBeforeRequests(t *testing.T) {
	d := material.Document{}
	topic := Topic{ID: "same-topic"}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("b%d", i)
		d.Pages = append(d.Pages, material.Page{Number: i, Image: []byte("image")})
		d.Blocks = append(d.Blocks, material.Block{ID: id, Page: i, Kind: "diagram", Text: "A rule with its diagram."})
		topic.BlockIDs = append(topic.BlockIDs, id)
		topic.Objectives = append(topic.Objectives, Objective{ID: id, BlockIDs: []string{id}})
	}
	tasks := splitTopicTasks(Plan{Topics: []Topic{topic}}, d, 24000)
	seen := 0
	for _, task := range tasks {
		if len(topicImages(task, d)) > 3 {
			t.Fatal("oversized visual task retained")
		}
		if task.ID != topic.ID {
			t.Fatal("topic identity changed")
		}
		seen += len(task.Objectives)
	}
	if len(tasks) < 2 || seen != 5 {
		t.Fatalf("objectives lost: %d in %d tasks", seen, len(tasks))
	}
}

func TestSharedBlockDoesNotImposeUnrelatedExampleScope(t *testing.T) {
	definition := "A secondary key can correspond to multiple records."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: definition + " For example, Library uses publication year."}}}
	topic := Topic{ID: "t", Title: "Secondary keys", BlockIDs: []string{"a"}, Objectives: []Objective{
		{ID: "definition", Text: "Definition", BlockIDs: []string{"a"}},
		{ID: "example", Text: "Example", Scope: []string{"Library"}, BlockIDs: []string{"a"}},
	}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:REVIEW") {
			return `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"a","kind":"text","quote":"` + definition + `"}],"covered":["definition"]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "How many records can share a secondary key value?", a: "Multiple records", quote: definition}), nil
	}}
	result := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "doc", Structure: &d}, topic, nil)
	if result.err != nil || len(result.Cards) != 1 {
		t.Fatalf("unrelated example scope rejected definition: %+v", result.Reviews)
	}
}

func TestPlanningCannotExcludeUsableDefinitionAsInsufficient(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Kind: "text", Text: "A secondary key is a set of attributes corresponding to a set of records. Multiple records can have the same secondary key value."}}}
	calls := 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		calls++
		if calls == 1 {
			return `{"topics":[],"excluded":[{"blockId":"a","kind":"insufficient","reason":"Definition lacks a section heading"}]}`, nil
		}
		return `{"topics":[{"id":"t","title":"Secondary key","blockIds":["a"],"objectives":[{"id":"o","text":"Definition","blockIds":["a"]}]}],"excluded":[]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || len(plan.Excluded) != 0 || len(plan.Topics) != 1 || calls != 2 {
		t.Fatalf("unjustified exclusion accepted: %+v %v calls=%d", plan, err, calls)
	}
}

func TestPlanningRetainsColumnConditionsEvenWithoutNamedScope(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "with", Page: 1, Kind: "heading", Text: "With index"},
		{ID: "first", Page: 1, Kind: "formula", Text: "log N"},
		{ID: "without", Page: 1, Kind: "heading", Text: "Without index"},
		{ID: "second", Page: 1, Kind: "formula", Text: "N"},
	}}
	p := Plan{Topics: []Topic{{ID: "t", BlockIDs: []string{"first", "second"}, Objectives: []Objective{{ID: "a", BlockIDs: []string{"first"}}, {ID: "b", BlockIDs: []string{"second"}}}}}}
	bindObjectiveScopes(&p, d)
	if !slices.Contains(p.Topics[0].Objectives[0].BlockIDs, "with") || !slices.Contains(p.Topics[0].Objectives[1].BlockIDs, "without") {
		t.Fatal("column conditions lost")
	}
	if slices.Contains(p.Topics[0].Objectives[1].BlockIDs, "with") {
		t.Fatal("borrowed condition from different column")
	}
}

func TestQuestionScopeAcceptsInflectionButNotAnotherConcept(t *testing.T) {
	for _, tt := range []struct {
		question, scope string
		want            bool
	}{
		{"Как выполняется поиск в плотном индексе?", "плотный индекс", true},
		{"Какая формула применяется на уровнях Б-дерева?", "Б-дерево", true},
		{"Как выполняется поиск в инвертируемом списке?", "Инвертируемые списки", true},
		{"Как выполняется поиск в неплотном индексе?", "плотный индекс", false},
		{"Как работает SQL?", "SQLite", false},
		{"Как выполняется поиск?", "плотный индекс", false},
	} {
		if got := questionContainsScope(tt.question, tt.scope); got != tt.want {
			t.Errorf("%q scope %q: got %v", tt.question, tt.scope, got)
		}
	}
}

func TestRepairFeedbackIsBoundedAndKeepsRecentReasons(t *testing.T) {
	var reviews []ReviewRecord
	for range 30 {
		reviews = append(reviews, ReviewRecord{TopicID: "t", Question: "Old question", Problem: strings.Repeat("Long explanation. ", 100), Evidence: []material.Evidence{{BlockID: "a", Quote: strings.Repeat("source", 100)}}})
	}
	reviews = append(reviews, ReviewRecord{TopicID: "t", Question: "Recent question", Problem: "Missing a necessary condition"})
	feedback := repairFeedback("t", reviews, 3000)
	if len(feedback) > 3000 || !strings.Contains(feedback, "Missing a necessary condition") || strings.Contains(feedback, "sourcesource") {
		t.Fatal("unbounded or stale repair feedback")
	}
	if reviews[0].Evidence == nil {
		t.Fatal("private report mutated")
	}
}

func TestInventedPlanScopeRemainsVisibleButCannotGenerateCards(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "CREATE INDEX creates an index."}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"topics":[{"id":"t","title":"Index creation","blockIds":["a"],"objectives":[{"id":"unsupported","text":"T-SQL syntax","scope":["T-SQL"],"blockIds":["a"]},{"id":"supported","text":"Index creation","blockIds":["a"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Topics[0].Objectives) != 2 || plan.Topics[0].Objectives[0].BlockedReason == "" {
		t.Fatal("unsupported goal hidden")
	}
	tasks := splitTopicTasks(plan, d, 24000)
	if len(tasks) != 1 || len(tasks[0].Objectives) != 1 || tasks[0].Objectives[0].ID != "supported" {
		t.Fatal("invented scope reached generation")
	}
}

func TestAcceptedAnswerNeedsIndependentSupportForEveryClause(t *testing.T) {
	quote := "N is the number of records."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: quote}}}
	topic := Topic{ID: "t", Title: "Notation", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", Text: "Notation", BlockIDs: []string{"a"}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:REVIEW") {
			return `{"verdicts":[{"index":0,"accept":false,"defect":"unsupported_answer","problem":"The extra claim about storage order has no source support","evidence":[{"blockId":"a","quote":"` + quote + `"}],"covered":[]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "What does N mean?", a: "The number of records; it also determines storage order", quote: quote}), nil
	}}
	r := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "doc", Structure: &d}, topic, nil)
	if r.err != nil || len(r.Cards) != 0 || len(r.Reviews) != 1 || !strings.Contains(r.Reviews[0].Problem, "extra claim") {
		t.Fatalf("unsupported extra clause survived: cards=%d reviews=%+v err=%v", len(r.Cards), r.Reviews, r.err)
	}
}

func TestMergeCannotCombineDifferentIndexConcepts(t *testing.T) {
	parts := []Plan{{Topics: []Topic{{ID: "dense", Title: "Плотный индекс", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "dense-goal", Text: "Dense", BlockIDs: []string{"a"}, Scope: []string{"Плотный индекс"}}}}}}, {Topics: []Topic{{ID: "sparse", Title: "Неплотный индекс", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "sparse-goal", Text: "Sparse", BlockIDs: []string{"b"}, Scope: []string{"Неплотный индекс"}}}}}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return `{"groups":[["dense","sparse"]]}`, nil }}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(plan.Topics) != 2 || len(plan.Issues) == 0 {
		t.Fatalf("different concepts silently merged: %+v %v", plan, err)
	}
}
