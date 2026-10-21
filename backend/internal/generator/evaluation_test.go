package generator

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// This is a controlled regression, not a quality measurement of a real model.
func TestControlledBaselineLosesScopeAndReviewRepairsIt(t *testing.T) {
	quote := "В SQLite действие по умолчанию для внешнего ключа — NO ACTION."
	wrong := mc{kind: "flip", q: "Какое действие внешнего ключа используется по умолчанию в любой СУБД?", a: "NO ACTION", quote: quote}
	old := NewGenerator(&fakeProvider{answer: func(int, string, providers.Request) (string, error) { return answerJSON(wrong), nil }}, Options{})
	baseline, err := old.Generate(context.Background(), Document{Text: quote})
	if err != nil || len(baseline.Cards) != 1 {
		t.Fatalf("baseline changed: %v %+v", err, baseline.Stats)
	}
	d := material.Document{Blocks: []material.Block{{ID: "p32-b1", Page: 32, Text: quote}}}
	generation := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		switch {
		case strings.Contains(r.System, "STAGE:PLAN"):
			return `{"topics":[{"id":"fk","title":"Ссылочная целостность","section":"SQLite","blockIds":["p32-b1"],"objectives":[{"id":"default","text":"Действие по умолчанию в SQLite","blockIds":["p32-b1"]}]}]}`, nil
		case strings.Contains(r.System, "STAGE:REVIEW"):
			accept := generation > 1
			problem := "Свойство SQLite ошибочно распространено на любую СУБД."
			if accept {
				problem = ""
			}
			b, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: accept, Problem: problem, Evidence: []material.Evidence{{BlockID: "p32-b1", Kind: "text", Quote: quote}}, Covered: []string{"default"}}}})
			return string(b), nil
		default:
			generation++
			if generation == 1 {
				return answerJSON(wrong), nil
			}
			if !strings.Contains(r.User, "ошибочно распространено") {
				t.Error("repair omitted concrete reviewer feedback")
			}
			wrong.q = "Какое действие внешнего ключа используется по умолчанию в SQLite?"
			return answerJSON(wrong), nil
		}
	}}
	result, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Structure: &d})
	if err != nil || len(result.Cards) != 1 || result.Stats.ReviewRejected != 1 || !strings.Contains(result.Cards[0].Question, "SQLite") {
		t.Fatalf("repair failed: %v %+v", err, result)
	}
	t.Logf("controlled baseline: %q; repaired: %q; no live model calls", baseline.Cards[0].Question, result.Cards[0].Question)
}

func TestVerifiedBatchDoesNotWaitForCoverageRepair(t *testing.T) {
	d := semanticDoc()
	plan := Plan{Topics: []Topic{{ID: "fk", Title: "Ключи", BlockIDs: []string{"p32-b1", "p33-b1"}, Objectives: []Objective{{ID: "a", Text: "Целостность", BlockIDs: []string{"p32-b1"}}, {ID: "b", Text: "SQLite", BlockIDs: []string{"p33-b1"}}}}}}
	released := make(chan struct{})
	first := make(chan struct{}, 1)
	generations := 0
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN") {
			b, _ := json.Marshal(plan)
			return string(b), nil
		}
		if strings.Contains(r.System, "STAGE:REVIEW") {
			b, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: true, Evidence: []material.Evidence{{BlockID: "p32-b1", Quote: "Внешний ключ обеспечивает ссылочную целостность."}}, Covered: []string{"a"}}}})
			return string(b), nil
		}
		generations++
		if generations > 1 {
			<-released
			return `{"facts":[]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Для чего служит внешний ключ?", a: "Обеспечивает ссылочную целостность.", quote: "Внешний ключ обеспечивает ссылочную целостность."}), nil
	}}
	done := make(chan error, 1)
	go func() {
		_, err := NewGenerator(p, Options{OnBatch: func(Batch) { first <- struct{}{} }}).Generate(context.Background(), d)
		done <- err
	}()
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		close(released)
		t.Fatal("first verified cards waited for repair")
	}
	close(released)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSyntheticMathConditionAndHandwritingDisagreement(t *testing.T) {
	// Synthetic transcription fixtures exercise grounding and question checking;
	// they cannot establish OCR accuracy on real LaTeX pages or handwriting.
	for _, tc := range []struct{ name, source, issue string }{
		{"typeset", "Если x > 0, то ln(x²) = 2 ln x. Доказательство использует положительность x.", ""},
		{"handwritten", "Если a ≠ 0, то ax = b имеет решение x = b/a.", "Символ a может быть α; сверить с исходным изображением"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := material.Document{Blocks: []material.Block{{ID: "p1-b1", Page: 1, Kind: "formula", Text: tc.source, Issues: []string{tc.issue}}}}
			topic := Topic{ID: "domain", Title: "Условия формулы", BlockIDs: []string{"p1-b1"}, Objectives: []Objective{{ID: "conditions", Text: "Сохранение условий", BlockIDs: []string{"p1-b1"}}}}
			source, err := topicSource(topic, d, 24000)
			if err != nil || !strings.Contains(source, tc.source) || tc.issue != "" && !strings.Contains(source, tc.issue) {
				t.Fatal("condition/uncertainty lost")
			}
			p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
				if !strings.Contains(r.User, tc.source) {
					t.Error("review received a summary instead of original formula")
				}
				b, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: false, Problem: "Потеряно условие применимости формулы; сомнительное обозначение нельзя угадывать.", Evidence: []material.Evidence{{BlockID: "p1-b1", Quote: tc.source}}}}})
				return string(b), nil
			}}
			vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Index: 0, Question: "Верно для любых значений?", Answer: "Да"}}, nil)
			if err != nil || len(vs) != 1 || vs[0].Accept {
				t.Fatalf("bad formula accepted: %v %+v", err, vs)
			}
		})
	}
}

func TestSQLDocumentReadingRegression(t *testing.T) {
	path := os.Getenv("POMNIBOT_EVAL_PDF")
	if path == "" {
		t.Skip("set POMNIBOT_EVAL_PDF to the supplied SQL lecture; no private document in git")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if material.Digest(data) != "124e265eabf075590bbaef74b6cf98af64c99482f6340cc093686bcafc3b0198" {
		t.Skip("fixture checksum differs")
	}
	began := time.Now()
	raw, err := exec.Command("pdftotext", "-enc", "UTF-8", "-eol", "unix", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	oldText := strings.ReplaceAll(string(raw), "\f", "\n")
	oldChunks := splitChunks(normalizeText(oldText), 2500, 150)
	baselineTime := time.Since(began)
	extractor, err := ingest.NewExtractor(nil, ingest.Options{AssessPDF: true})
	if err != nil {
		t.Fatal(err)
	}
	began = time.Now()
	read, err := extractor.Extract(context.Background(), []ingest.File{{Name: "lecture.pdf", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Pages) != 36 {
		t.Fatalf("pages=%d", len(read.Pages))
	}
	for _, n := range []int{3, 4, 5, 7, 12, 17, 22, 27, 28, 29, 31, 32, 33, 34, 35, 36} {
		if len(read.Pages[n-1].Issues) == 0 {
			t.Errorf("risky page %d incorrectly deemed complete without vision", n)
		}
	}
	if strings.Contains(read.Document.Pages[11].Reading, "бинарных отношений") {
		t.Fatal("local-only run invented image-only content")
	}
	if !strings.Contains(read.Document.Pages[30].Reading, "ELECT") {
		t.Fatal("silently repaired cropped SELECT")
	}
	t.Logf("local-only: baseline chunks=%d baseline time=%s; new pages=%d blocks=%d warnings=%d local time=%s; live quality/latency unmeasured", len(oldChunks), baselineTime, len(read.Pages), len(read.Document.Blocks), len(read.Warnings), time.Since(began))
}

func TestSQLRelatedContextRegression(t *testing.T) {
	path := os.Getenv("POMNIBOT_EVAL_READING")
	if path == "" {
		t.Skip("explicit private SQL reading snapshot required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Ingest ingest.Result `json:"ingest"`
		Plan   Plan          `json:"plan"`
	}
	if err = json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	relatedCode(&recorded.Plan, recorded.Ingest.Document)
	linked := false
	for _, topic := range recorded.Plan.Topics {
		has32, has22 := false, false
		for _, id := range topic.BlockIDs {
			if b, ok := recorded.Ingest.Document.Block(id); ok && b.Page == 32 {
				has32 = true
			}
		}
		for _, id := range topic.RelatedBlockIDs {
			if b, ok := recorded.Ingest.Document.Block(id); ok && b.Page == 22 {
				has22 = true
			}
		}
		linked = linked || has32 && has22
	}
	if !linked {
		t.Fatal("real FK actions still lack the distant declaration example")
	}
	if output := os.Getenv("POMNIBOT_EVAL_MAP_OUTPUT"); output != "" {
		type entry struct {
			Title        string `json:"title"`
			Pages        []int  `json:"pages"`
			RelatedPages []int  `json:"relatedPages"`
			Objectives   int    `json:"objectives"`
		}
		var entries []entry
		for _, topic := range recorded.Plan.Topics {
			e := entry{Title: topic.Title, Objectives: len(topic.Objectives)}
			for _, id := range topic.BlockIDs {
				if b, ok := recorded.Ingest.Document.Block(id); ok && !slices.Contains(e.Pages, b.Page) {
					e.Pages = append(e.Pages, b.Page)
				}
			}
			for _, id := range topic.RelatedBlockIDs {
				if b, ok := recorded.Ingest.Document.Block(id); ok && !slices.Contains(e.RelatedPages, b.Page) {
					e.RelatedPages = append(e.RelatedPages, b.Page)
				}
			}
			slices.Sort(e.Pages)
			slices.Sort(e.RelatedPages)
			entries = append(entries, e)
		}
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
