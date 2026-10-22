package generator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestSupplementPlansOnlyMissingBlocksAndPreservesOriginalGoals(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Definition."}, {ID: "p15-b6", Text: "Maximum disk accesses."}, {ID: "p22-b1", Text: "Index recommendations."}, {ID: "p27-b3", Kind: "image", Text: "Menu command."}}}
	p := &fakeProvider{answer: func(n int, _ string, r providers.Request) (string, error) {
		if n == 1 {
			return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"hard","text":"Difficult definition","blockIds":["a"],"priority":"core","scope":[]}]}]}`, nil
		}
		if !strings.Contains(r.System, "STAGE:PLAN-SUPPLEMENT") {
			t.Error("retry must request additive planning")
		}
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["p15-b6"],"objectives":[]},{"id":"extra","title":"Tools","blockIds":["p22-b1","p27-b3"],"objectives":[{"id":"new","text":"Tools","blockIds":["p22-b1","p27-b3"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || len(unassignedBlocks(plan, d)) != 0 || len(plan.Topics) != 2 || plan.Topics[0].Objectives[0].ID != "hard" || plan.Topics[0].Objectives[0].Priority != "core" {
		t.Fatalf("lost goals or missing blocks: %+v %v", plan, err)
	}
}

func TestContradictoryRejectionIsCorrectedWithoutAutomaticAcceptance(t *testing.T) {
	for _, badEvidence := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "invented evidence"}[badEvidence], func(t *testing.T) {
			d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Indexes are primary or secondary."}}}
			topic := Topic{ID: "t", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", BlockIDs: []string{"a"}}}}
			calls := 0
			p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
				calls++
				v := verdict{Index: 0, Defect: "none", Problem: "Отклонение не обосновано: открытый вопрос допустим.", Evidence: []material.Evidence{{BlockID: "a", Quote: d.Blocks[0].Text}}}
				if strings.Contains(r.System, "VERDICT-CONSISTENCY") {
					v.Accept = true
					v.Problem = ""
					v.Covered = []string{"o"}
					if badEvidence {
						v.Evidence[0].Quote = "Invented"
					}
				}
				b, _ := json.Marshal(map[string]any{"verdicts": []verdict{v}})
				return string(b), nil
			}}
			vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Question: "How are indexes classified?", Answer: "Primary or secondary"}}, nil)
			if err != nil || len(vs) != 1 || vs[0].Accept == badEvidence || calls != 2 {
				t.Fatalf("contradictory verdict unresolved or unsafe: %+v calls=%d err=%v", vs, calls, err)
			}
		})
	}
}

func TestSemanticGenerationRejectsRecallOfPrivateExample(t *testing.T) {
	quote := "В примере БД «Библиотека» вторичный ключ — год издания."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: quote}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return answerJSON(mc{kind: "flip", q: "Какой атрибут служит вторичным ключом в примере БД «Библиотека»?", a: "Год издания", quote: quote}), nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: quote, EvidenceSource: &d})
	if err != nil || len(r.Cards) != 0 || r.Stats.DroppedExample != 1 {
		t.Fatalf("example recall survived: cards=%d stats=%+v err=%v", len(r.Cards), r.Stats, err)
	}
}

func TestMergeRechecksDifferentTitlesForSameOperation(t *testing.T) {
	parts := []Plan{{Topics: []Topic{{ID: "a", Title: "Добавление записи", Section: "Плотный индекс", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "Добавление при свободном блоке", BlockIDs: []string{"a"}}}}}}, {Topics: []Topic{{ID: "b", Title: "Переполнение блока", Section: "Плотный индекс", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "Добавление при переполнении", BlockIDs: []string{"b"}}}}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["a","b"],"reason":"Два случая одной операции добавления в плотный индекс."}]}`, nil
		}
		return `{"groups":[["a","b"]]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(plan.Topics) != 1 || len(plan.Topics[0].Objectives) != 2 || len(p.requests) != 2 {
		t.Fatalf("same operation split: %+v %v calls=%d", plan, err, len(p.requests))
	}
}

func TestMergeDoesNotAbandonValidSiblingGroup(t *testing.T) {
	parts := []Plan{{Topics: []Topic{
		{ID: "dense", Title: "Плотный индекс", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "Определение плотного индекса", BlockIDs: []string{"a"}}}},
		{ID: "insert", Title: "Добавление записи", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "Добавление в свободный блок", BlockIDs: []string{"b"}}}},
	}}, {Topics: []Topic{
		{ID: "sparse", Title: "Неплотный индекс", BlockIDs: []string{"c"}, Objectives: []Objective{{ID: "oc", Text: "Определение неплотного индекса", BlockIDs: []string{"c"}}}},
		{ID: "overflow", Title: "Переполнение блока", BlockIDs: []string{"d"}, Objectives: []Objective{{ID: "od", Text: "Добавление при переполнении", BlockIDs: []string{"d"}}}},
	}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["dense"],"reason":"Плотный индекс"},{"topicIds":["sparse"],"reason":"Неплотный индекс"},{"topicIds":["insert","overflow"],"reason":"Одна операция добавления"}]}`, nil
		}
		return `{"groups":[["dense","sparse","insert","overflow"]]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	goals := 0
	for _, topic := range plan.Topics {
		goals += len(topic.Objectives)
	}
	if err != nil || len(plan.Topics) != 3 || goals != 4 {
		t.Fatalf("valid sibling grouping lost: %+v %v", plan, err)
	}
}

func TestExampleGateAllowsSelfContainedApplicationAndDefinitions(t *testing.T) {
	for _, q := range []string{
		"В примере дана таблица:\n| имя | год |\n| А | 2020 |\n| Б | 2020 |\nКакой атрибут может быть вторичным ключом и почему?",
		"Какие виды ключей различаются по уникальности?",
		"Почему вторичный ключ может соответствовать нескольким записям?",
	} {
		if recallsExample(modelCard{Question: q}) {
			t.Errorf("valid learning question blocked: %s", q)
		}
	}
}

func TestMergeChecksOperationEvenWithSharedTitleWords(t *testing.T) {
	parts := []Plan{{Topics: []Topic{{ID: "search", Title: "Поиск в плотном индексе", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "Алгоритм поиска", BlockIDs: []string{"a"}}}}}}, {Topics: []Topic{{ID: "delete", Title: "Удаление в плотном индексе", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "Алгоритм удаления", BlockIDs: []string{"b"}}}}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["search"],"reason":"Поиск"},{"topicIds":["delete"],"reason":"Удаление"}]}`, nil
		}
		return `{"groups":[["search","delete"]]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(plan.Topics) != 2 {
		t.Fatalf("different operations collapsed: %+v %v", plan, err)
	}
}

func TestPersistentContradictionFailsClosedAfterOneRecheck(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "An index speeds up search."}}}
	calls := 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		calls++
		return `{"verdicts":[{"index":0,"accept":false,"defect":"none","problem":"No defect.","evidence":[],"covered":[]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, Topic{BlockIDs: []string{"a"}}, []reviewCandidate{{Question: "What does an index speed up?", Answer: "Search"}}, nil)
	if err != nil || len(vs) != 1 || vs[0].Accept || calls != 2 {
		t.Fatalf("unbounded or unsafe contradiction handling: %+v calls=%d err=%v", vs, calls, err)
	}
}

func TestGroupingChecksUseOneBatchInsteadOfQuadraticCalls(t *testing.T) {
	parts := []Plan{{Topics: []Topic{}}, {Topics: []Topic{}}}
	for _, id := range []string{"a", "b", "c", "d"} {
		parts[0].Topics = append(parts[0].Topics, Topic{ID: id, Title: id, BlockIDs: []string{id}, Objectives: []Objective{{ID: "o" + id, Text: id, BlockIDs: []string{id}}}})
	}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["a","b"],"reason":"Одна операция"},{"topicIds":["c","d"],"reason":"Другая операция"}]}`, nil
		}
		return `{"groups":[["a","b","c","d"]]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(plan.Topics) != 2 || len(p.requests) != 2 {
		t.Fatalf("group verification is not bounded to one batch: topics=%d calls=%d err=%v", len(plan.Topics), len(p.requests), err)
	}
}

func TestGroupingVerificationCannotLoseOrInventMembership(t *testing.T) {
	parts := []Plan{{Topics: []Topic{
		{ID: "a", Title: "A", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "A", BlockIDs: []string{"a"}}}},
		{ID: "b", Title: "B", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "B", BlockIDs: []string{"b"}}}},
		{ID: "c", Title: "C", BlockIDs: []string{"c"}, Objectives: []Objective{{ID: "oc", Text: "C", BlockIDs: []string{"c"}}}},
	}}}
	for _, response := range []string{
		`{"groups":[{"topicIds":["a"],"reason":"A"}]}`,
		`{"groups":[{"topicIds":["a","b","a","c"],"reason":"Repeated"}]}`,
		`{"groups":[{"topicIds":["a","b","unknown"],"reason":"Invented"}]}`,

		`{"groups":[{"topicIds":["a","b"],"reason":""},{"topicIds":["c"],"reason":"C"}]}`,
	} {
		p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
			if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
				return response, nil
			}
			return `{"groups":[["a","b"],["c"]]}`, nil
		}}
		plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
		if err != nil || len(plan.Topics) != 3 || len(plan.Issues) == 0 || len(p.requests) != 2 {
			t.Fatalf("invalid confirmation changed plan: %+v %v", plan, err)
		}
	}
}

func TestQuestionRetainsSourcePageConditionsBeforeReview(t *testing.T) {
	quote := "Tпоиска = Log 2 (N / (LB / LI)) + 1"
	d := material.Document{Blocks: []material.Block{
		{ID: "title", Page: 10, Kind: "heading", Text: "Плотный индекс"},
		{ID: "formula", Page: 10, Kind: "formula", Text: quote},
		{ID: "qualifier", Page: 10, Kind: "text", Text: "Оценка времени на выполнение основных операций\n(в максимальном количестве обращений к диску)"},
	}}
	topic := Topic{ID: "t", Title: "Оценка", BlockIDs: []string{"formula"}, Objectives: []Objective{{ID: "o", Text: "Поиск", BlockIDs: []string{"formula"}}}}
	reviews := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:REVIEW") {
			reviews++
			var input struct {
				Cards []reviewCandidate `json:"cards"`
			}
			_ = json.Unmarshal([]byte(r.User), &input)
			if !strings.Contains(input.Cards[0].Question, "максимальном количестве обращений к диску") || !strings.Contains(input.Cards[0].Question, "Плотный индекс") {
				t.Error("question lost source context")
			}
			return `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"formula","quote":"` + quote + `"}],"covered":["o"]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Как записывается формула оценки времени поиска?", a: "Log 2 (N / (LB / LI)) + 1", quote: quote}), nil
	}}
	result := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "doc", Structure: &d}, topic, nil)
	if result.err != nil || len(result.Cards) != 1 || reviews != 1 || !strings.Contains(result.Cards[0].Question, "максимальном количестве обращений к диску") {
		t.Fatalf("source context was not retained: %+v %v", result.Cards, result.err)
	}
}

func TestRecheckDoesNotAnchorOnFalseReasonAndValidatesDefect(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "В SQLite действие выполняется сразу."}}}
	topic := Topic{ID: "t", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "o", Text: "Момент", BlockIDs: []string{"a"}}}}
	calls := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		calls++
		if calls == 1 || strings.Contains(r.User, "Не указан SQLite") {
			return `{"verdicts":[{"index":0,"accept":false,"defect":"missing_condition","defectQuote":"SQLite","problem":"Не указан SQLite","evidence":[{"blockId":"a","quote":"В SQLite действие выполняется сразу."}],"covered":[]}]}`, nil
		}
		return `{"verdicts":[{"index":0,"accept":true,"defect":"none","defectQuote":"","problem":"","evidence":[{"blockId":"a","quote":"В SQLite действие выполняется сразу."}],"covered":["o"]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Question: "В SQLite когда выполняется действие?", Answer: "Сразу"}}, nil)
	if err != nil || len(vs) != 1 || !vs[0].Accept || calls != 2 {
		t.Fatalf("false reason anchors recheck: %+v calls=%d %v", vs, calls, err)
	}
}

func TestQuestionContextDoesNotBorrowAnotherSectionOrRevealAnswer(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "dense", Page: 1, Kind: "heading", Text: "Плотный индекс"},
		{ID: "rule", Page: 1, Kind: "text", Text: "Бинарный поиск."},
		{ID: "sparse", Page: 1, Kind: "heading", Text: "Неплотный индекс"},
		{ID: "caption", Page: 1, Kind: "text", Text: "Другая оценка\n(при ином условии)"},
	}}
	c, _ := bindCardContext(cards.Card{Question: "Как выполняется поиск?", Answer: "Бинарный поиск", Evidence: []material.Evidence{{BlockID: "rule", Quote: "Бинарный поиск."}}}, d)
	if strings.Contains(c.Question, "Неплотный") || strings.Contains(c.Question, "ином условии") {
		t.Fatalf("borrowed another section: %s", c.Question)
	}
	c, _ = bindCardContext(cards.Card{Question: "Какой вид индекса описан?", Answer: "Плотный индекс", Evidence: []material.Evidence{{BlockID: "rule", Quote: "Бинарный поиск."}}}, d)
	if strings.Contains(c.Question, "Плотный индекс") {
		t.Fatalf("context revealed answer: %s", c.Question)
	}
}

func TestCausalQuestionCannotBeAnsweredByRepeatingItsPremise(t *testing.T) {
	for _, tt := range []struct{ q, a, quote string }{
		{"Почему при частом изменении записей большое количество вторичных индексов может замедлить обработку информации?", "Наличие большого количества вторичных индексов может резко замедлить процесс обработки информации.", "При частом изменении записей большое количество вторичных индексов может резко замедлить процесс обработки информации."},
		{"Почему в индекс целесообразно включать столбцы, которые обновляются редко?", "В индекс целесообразно включать редко обновляемые столбцы, так как часто обновляемые столбцы ухудшают индекс.", "В индекс включаются редко обновляемые столбцы."},
	} {
		d := material.Document{Blocks: []material.Block{{ID: "a", Text: tt.quote}}}
		p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
			return answerJSON(mc{kind: "flip", q: tt.q, a: tt.a, quote: tt.quote}), nil
		}}
		r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: tt.quote, EvidenceSource: &d})
		if err != nil || len(r.Cards) != 0 {
			t.Fatalf("nonanswer survived: %+v %v", r.Cards, err)
		}
	}
}

func TestScopeGroundingAllowsHyphenSpacingWithoutAcceptingDifferentNames(t *testing.T) {
	if !containsScope("Оператор в T - SQL", "T-SQL") || !questionContainsScope("В T-SQL что делает оператор?", "T - SQL") {
		t.Error("spacing falsely invents unsupported scope")
	}
	if containsScope("T - SQLx", "T-SQL") || containsScope("неплотный индекс", "плотный индекс") {
		t.Error("different source name accepted")
	}
}

func TestSupplementIgnoresRepeatedExistingExclusion(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Definition."}, {ID: "b", Text: "Necessary condition."}, {ID: "service", Kind: "heading", Text: "Contents"}}}
	calls := 0
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		calls++
		if calls == 1 {
			return `{"topics":[{"id":"t","title":"Rule","blockIds":["a"],"objectives":[{"id":"o","text":"Definition","blockIds":["a"]}]}],"excluded":[{"blockId":"service","kind":"service","reason":"Navigation"}]}`, nil
		}
		return `{"topics":[{"id":"t","title":"Rule","blockIds":["b"],"objectives":[]}],"excluded":[{"blockId":"service","kind":"service","reason":"Navigation"}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{}).plan(context.Background(), d, nil)
	if err != nil || len(unassignedBlocks(plan, d)) > 0 || len(plan.Excluded) != 1 || plan.Topics[0].Objectives[0].ID != "o" {
		t.Fatalf("duplicate exclusion discards valid supplement: %+v %v", plan, err)
	}
}

func TestCausalRestatementPreservesShortExplanationsWithAReason(t *testing.T) {
	for _, tt := range []struct{ q, a string }{
		{"Почему для разных критериев поиска создаются разные индексы?", "Потому что таблицу нельзя отсортировать сразу по нескольким критериям."},
		{"Почему используется эта структура?", "Потому что дешевле."},
		{"Почему таблица сортируется?", "Чтобы ускорить поиск."},
		{"Какие столбцы рекомендуется включать в индекс?", "Редко обновляемые."},
	} {
		if causalRestatement(modelCard{Question: tt.q, Answer: tt.a}) {
			t.Errorf("valid short explanation blocked: %s", tt.a)
		}
	}
}

func TestPlanningAssignsTrailingSourceQualifierWithoutNewGoals(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "heading", Page: 15, Kind: "heading", Text: "Неплотный индекс"},
		{ID: "formula", Page: 15, Kind: "formula", Text: "T = N + 1"},
		{ID: "p15-b6", Page: 15, Kind: "text", Text: "Оценка времени операций\n(в максимальном количестве обращений к диску)"},
	}}
	plan := Plan{Topics: []Topic{{ID: "t", Title: "Оценка", BlockIDs: []string{"formula"}, Objectives: []Objective{{ID: "original", Text: "Оценка", Priority: "core", BlockIDs: []string{"formula"}}}}}}
	bindObjectiveScopes(&plan, d)
	if !strings.Contains(strings.Join(plan.Topics[0].BlockIDs, ","), "p15-b6") || len(plan.Topics[0].Objectives) != 1 || plan.Topics[0].Objectives[0].ID != "original" || plan.Topics[0].Objectives[0].Priority != "core" {
		t.Fatalf("source qualifier remains unassigned or goals changed: %+v", plan)
	}
}

func TestContradictionDetectionUsesStructuredDefect(t *testing.T) {
	if !contradictoryRejection(verdict{Problem: "No defect", Defect: "none"}) {
		t.Error("structured contradiction was missed")
	}
	if contradictoryRejection(verdict{Problem: "review cites unsupported evidence", Defect: "none"}) {
		t.Error("grounding failure is not a semantic appeal")
	}
}

func TestCausalRestatementChecksQuestionAfterHeading(t *testing.T) {
	c := modelCard{Question: "Инструмент настройки индексов.\n\nПочему множество индексов может замедлить обработку?", Answer: "Наличие множества индексов может резко замедлить процесс обработки."}
	if !causalRestatement(c) {
		t.Error("heading bypasses causal answer check")
	}
}

func TestQuestionContextDoesNotTurnColumnHeadingIntoPageScope(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "title", Page: 11, Kind: "heading", Text: "Неплотный индекс"},
		{ID: "column", Page: 11, Kind: "heading", Text: "Основная таблица"},
		{ID: "footer", Page: 11, Kind: "text", Text: "Поиск в индексном файле выполняется бинарным поиском."},
	}}
	c, _ := bindCardContext(cards.Card{Question: "Как выполняется поиск в неплотном индексе?", Answer: "Бинарным поиском в индексном файле", Evidence: []material.Evidence{{BlockID: "footer", Quote: d.Blocks[2].Text}}}, d)
	if strings.Contains(c.Question, "Основная таблица") {
		t.Fatalf("column label changed question scope: %s", c.Question)
	}
}
