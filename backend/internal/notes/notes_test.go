package notes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type fakeExtractor struct {
	result ingest.Result
	err    error
}

func (f *fakeExtractor) Extract(context.Context, []ingest.File) (ingest.Result, error) {
	if f.err != nil {
		return ingest.Result{}, f.err
	}
	if f.result.Text == "" && f.result.Warnings == nil {
		return ingest.Result{Text: "Митоз состоит из четырёх фаз."}, nil
	}
	return f.result, nil
}

type fakeGenerator struct {
	generate func(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error)
}

func oneCard(topic string) generator.Result {
	name := topic
	if name == "" {
		name = "Митоз"
	}
	return generator.Result{Facts: []generator.Fact{{Name: name, Pages: []int{1}, Cards: []generator.Card{generator.FlipCard{CardBase: generator.CardBase{Question: "Сколько фаз у митоза?", Source: "Митоз"}, Answer: "Четыре"}}}}}
}

func (f *fakeGenerator) Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
	if f.generate != nil {
		return f.generate(ctx, doc, onBatch)
	}
	res := oneCard("")
	if onBatch != nil {
		onBatch(generator.Batch{Facts: res.Facts})
	}
	return res, nil
}

type fakeSaver struct {
	mu     sync.Mutex
	titles []string
	err    error
}

func (f *fakeSaver) SaveGeneratedSet(_ context.Context, _ uuid.UUID, title string, _ generator.Result) (*contracts.CardSet, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.titles = append(f.titles, title)
	return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
}

func file(name string) []ingest.File {
	return []ingest.File{{Name: name, Data: []byte("notes")}}
}

func TestMake_SavesSetNamedAfterFile(t *testing.T) {
	saver := &fakeSaver{}
	extractor := &fakeExtractor{result: ingest.Result{Text: "Митоз.", Warnings: []string{"Страница 2 размыта"}}}
	m := New(extractor, &fakeGenerator{}, saver, 1)

	var cardsSeen []int
	res, err := m.Make(context.Background(), Request{UserID: uuid.New(), Files: file("Лекция_4_митоз.pdf")},
		Progress{Cards: func(total int) { cardsSeen = append(cardsSeen, total) }})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if res.Set == nil || res.Set.Title != "Лекция 4 митоз" {
		t.Errorf("set = %+v, want one titled «Лекция 4 митоз»", res.Set)
	}
	if !slices.Equal(res.Warnings, []string{"Страница 2 размыта"}) {
		t.Errorf("warnings = %q, want the extractor's", res.Warnings)
	}
	if !slices.Equal(cardsSeen, []int{1}) {
		t.Errorf("Cards progress = %v, want [1]", cardsSeen)
	}
}

func TestMake_CaptionNamesTheSet(t *testing.T) {
	saver := &fakeSaver{}
	m := New(&fakeExtractor{}, &fakeGenerator{}, saver, 1)
	res, err := m.Make(context.Background(), Request{Caption: "Биология\nвторая строка", Files: file("IMG_1489.HEIC")}, Progress{})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if res.Set.Title != "Биология" {
		t.Errorf("title = %q, want the first line of the caption", res.Set.Title)
	}
}

// A photo with no caption is named after its commonest topic.
func TestMake_GenericNameTakesTopic(t *testing.T) {
	gen := &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		return oneCard("Фазы митоза"), nil
	}}
	m := New(&fakeExtractor{}, gen, &fakeSaver{}, 1)
	res, err := m.Make(context.Background(), Request{Files: file("IMG_1489.HEIC")}, Progress{})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if res.Set.Title != "Фазы митоза" {
		t.Errorf("title = %q, want the topic", res.Set.Title)
	}
}

func TestMake_Failures(t *testing.T) {
	failing := func(err error, stats generator.Stats) *fakeGenerator {
		return &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
			return generator.Result{Stats: stats}, err
		}}
	}
	twoFiles := append(file("a.pdf"), ingest.File{Name: "b.docx", Data: []byte("x")})

	tests := []struct {
		name      string
		files     []ingest.File
		extractor *fakeExtractor
		gen       *fakeGenerator
		saveErr   error
		want      string
	}{
		{name: "no files", files: []ingest.File{}, want: TextResend},
		{name: "ingest error names the file among several", files: twoFiles,
			extractor: &fakeExtractor{err: &ingest.Error{File: "b.docx", Err: ingest.ErrUnsupported, Message: "Формат не поддерживается."}},
			want:      "😕 Файл «b.docx»: Формат не поддерживается."},
		{name: "ingest error, one file",
			extractor: &fakeExtractor{err: &ingest.Error{File: "a.pdf", Err: ingest.ErrUnsupported, Message: "Формат не поддерживается."}},
			want:      "😕 Формат не поддерживается."},
		{name: "unknown extract error", extractor: &fakeExtractor{err: errors.New("boom")}, want: TextTryLater},
		{name: "no text", extractor: &fakeExtractor{result: ingest.Result{Text: " \n ", Warnings: []string{}}}, want: TextNoText},
		{name: "model fails", gen: failing(errors.New("503"), generator.Stats{}), want: TextTryLater},
		{name: "refused", gen: failing(providers.ErrRefused, generator.Stats{}), want: TextOtherText},
		{name: "no card kept", gen: failing(nil, generator.Stats{CardsFromModel: 6, DroppedQuote: 6}), want: TextNoCards},
		{name: "saving fails", saveErr: errors.New("db down"), want: TextTryLater},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor, gen := tt.extractor, tt.gen
			if extractor == nil {
				extractor = &fakeExtractor{}
			}
			if gen == nil {
				gen = &fakeGenerator{}
			}
			files := tt.files
			if files == nil {
				files = file("a.pdf")
			}
			saver := &fakeSaver{err: tt.saveErr}
			_, err := New(extractor, gen, saver, 1).Make(context.Background(), Request{Files: files}, Progress{})
			if err == nil {
				t.Fatal("Make: no error")
			}
			if got := FailureText(err); got != tt.want {
				t.Errorf("FailureText = %q, want %q", got, tt.want)
			}
			if tt.saveErr == nil && len(saver.titles) > 0 {
				t.Errorf("a failed document was saved as %q", saver.titles)
			}
		})
	}
}

// The second document waits for the first, is told how many are ahead, and
// is saved after it: the slot is held until the set is saved.
func TestMake_Queue(t *testing.T) {
	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	gen := &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		started <- struct{}{}
		<-unblock
		return oneCard(""), nil
	}}
	saver := &fakeSaver{}
	m := New(&fakeExtractor{}, gen, saver, 1)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := m.Make(ctx, Request{Files: file("first.pdf")}, Progress{
			Queued: func(int) { t.Error("the first document was queued with a free slot") },
		}); err != nil {
			t.Errorf("first: %v", err)
		}
	})
	<-started

	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	queued := make(chan struct{})
	wg.Go(func() {
		if _, err := m.Make(ctx, Request{Files: file("second.pdf")}, Progress{
			Queued:     func(ahead int) { record(fmt.Sprintf("queued %d", ahead)); close(queued) },
			Reading:    func() { record("reading") },
			Generating: func() { record("generating") },
		}); err != nil {
			t.Errorf("second: %v", err)
		}
	})
	<-queued

	select {
	case <-started:
		t.Fatal("the second document started while the first held the slot")
	case <-time.After(50 * time.Millisecond):
	}
	close(unblock)
	wg.Wait()

	if want := []string{"queued 1", "reading", "generating"}; !slices.Equal(events, want) {
		t.Errorf("events = %q, want %q", events, want)
	}
	if !slices.Equal(saver.titles, []string{"first", "second"}) {
		t.Errorf("saved = %q, want [first second] in arrival order", saver.titles)
	}
}

// A document that was not queued is not told «your turn».
func TestMake_NoReadingEventWithoutWaiting(t *testing.T) {
	var events []string
	_, err := New(&fakeExtractor{}, &fakeGenerator{}, &fakeSaver{}, 1).Make(context.Background(), Request{Files: file("a.pdf")}, Progress{
		Reading:    func() { events = append(events, "reading") },
		Generating: func() { events = append(events, "generating") },
	})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !slices.Equal(events, []string{"generating"}) {
		t.Errorf("events = %q, want [generating]", events)
	}
}

// A student who gives up waiting gets no message about it.
func TestMake_GaveUpWaiting(t *testing.T) {
	started := make(chan struct{})
	unblock := make(chan struct{})
	defer close(unblock)
	gen := &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		close(started)
		<-unblock
		return oneCard(""), nil
	}}
	m := New(&fakeExtractor{}, gen, &fakeSaver{}, 1)
	go func() { _, _ = m.Make(context.Background(), Request{Files: file("first.pdf")}, Progress{}) }()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	_, err := m.Make(ctx, Request{Files: file("second.pdf")}, Progress{Queued: func(int) { cancel() }})
	if !errors.Is(err, ErrGaveUp) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrGaveUp and context.Canceled", err)
	}
	if got := FailureText(err); got != "" {
		t.Errorf("FailureText = %q, want none", got)
	}
}

// A Maker with no slots still lets documents through, one at a time.
func TestMake_SlotsAtLeastOne(t *testing.T) {
	for _, slots := range []int{0, -1} {
		m := New(&fakeExtractor{}, &fakeGenerator{}, &fakeSaver{}, slots)
		done := make(chan error, 1)
		go func() {
			_, err := m.Make(context.Background(), Request{Files: file("a.pdf")}, Progress{})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("slots %d: %v", slots, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("slots %d: Make never got a slot", slots)
		}
	}
}

func TestWithWarnings(t *testing.T) {
	got := WithWarnings("Готово", []string{"1", "2", "3", "4", "5"})
	if !strings.Contains(got, "⚠️ 1\n⚠️ 2\n⚠️ 3") || !strings.Contains(got, "…и ещё 2") || strings.Contains(got, "⚠️ 4") {
		t.Errorf("WithWarnings = %q, want three warnings and «…и ещё 2»", got)
	}
	if got := WithWarnings("Готово", nil); got != "Готово" {
		t.Errorf("WithWarnings without warnings = %q", got)
	}
}

func TestMakeWarnsWhenSourceBlocksRemainUnassigned(t *testing.T) {
	gen := &fakeGenerator{generate: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		r := oneCard("")
		r.Coverage.Unassigned = []string{"b"}
		return r, nil
	}}
	extractor := &fakeExtractor{result: ingest.Result{Text: "Митоз.", Document: material.Document{Blocks: []material.Block{{ID: "b", Text: "Митоз."}}}}}
	m := New(extractor, gen, &fakeSaver{}, 1)
	res, err := m.Make(context.Background(), Request{UserID: uuid.New(), Files: file("lecture.pdf")}, Progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "Набор частичный") {
		t.Fatalf("hidden partial result: %+v", res)
	}
}

func TestMakeNativePDFWithoutTextAndNeverSavesFailedGeneration(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "save validated", true: "reject generation"}[failed], func(t *testing.T) {
			saver := &fakeSaver{}
			ex := &fakeExtractor{result: ingest.Result{PDFs: []providers.PDF{{Name: "scan.pdf", Data: []byte("%PDF"), Pages: 2}}, Warnings: []string{}}}
			gen := &fakeGenerator{generate: func(_ context.Context, doc generator.Document, _ func(generator.Batch)) (generator.Result, error) {
				if len(doc.PDFs) != 1 || doc.PDFs[0].Pages != 2 {
					t.Fatalf("lost originals: %+v", doc)
				}
				if failed {
					return oneCard("T"), errors.New("generation failed")
				}
				return oneCard("T"), nil
			}}
			_, err := New(ex, gen, saver, 1).Make(context.Background(), Request{UserID: uuid.New(), Files: file("scan.pdf")}, Progress{})
			if failed {
				if err == nil || len(saver.titles) != 0 {
					t.Fatal("saved failed generation")
				}
			} else if err != nil || len(saver.titles) != 1 {
				t.Fatalf("not saved: %v", err)
			}
		})
	}
}
