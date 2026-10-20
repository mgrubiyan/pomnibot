// Package notes turns the files a student sends into a saved card set: it
// waits its turn at the model, reads the files, makes the cards, names the
// set and saves it. A bot only downloads the files and tells the student how
// it goes; the texts for that are here too, so every bot says the same.
//
//	maker := notes.New(extractor, notes.FromGenerator(gen), sets, slots)
//	res, err := maker.Make(ctx, notes.Request{UserID: id, Files: files}, notes.Progress{...})
//	if err != nil {
//		send(notes.FailureText(err))
//	}
//
// Build one Maker and hand it to every bot: they share its queue, so
// together they stay within what the model serves at once.
package notes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
)

// Extractor reads text from files and photos.
type Extractor interface {
	Extract(ctx context.Context, files []ingest.File) (ingest.Result, error)
}

// Generator makes cards from text, passing them to onBatch as they are ready.
type Generator interface {
	Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error)
}

// Saver saves the cards as a new set of the user.
type Saver interface {
	SaveGeneratedSet(ctx context.Context, userID int64, title string, genResult generator.Result) (*contracts.CardSet, error)
}

type fromGenerator struct {
	gen *generator.Generator
}

func (g fromGenerator) Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
	gen := g.gen
	if onBatch != nil {
		gen = gen.WithOnBatch(onBatch)
	}
	return gen.Generate(ctx, doc)
}

// FromGenerator adapts g to Generator.
func FromGenerator(g *generator.Generator) Generator {
	return fromGenerator{gen: g}
}

// Maker turns files into saved sets. It is safe for concurrent use.
type Maker struct {
	queue     *generator.Queue
	extractor Extractor
	generator Generator
	sets      Saver
}

// New returns a Maker that works on slots documents at once, at least one:
// as many requests as the main model serves in parallel. Reading a document
// and making its cards both call the model. All three dependencies are
// required.
func New(extractor Extractor, gen Generator, sets Saver, slots int) *Maker {
	return &Maker{
		queue:     generator.NewQueue(slots),
		extractor: extractor,
		generator: gen,
		sets:      sets,
	}
}

// Request is one document: the files a student sent at once, already
// downloaded, and the caption under them.
type Request struct {
	UserID  int64
	Caption string
	Files   []ingest.File
}

// Progress tells the bot how the document goes, so it can tell the student.
// Every field may be nil.
type Progress struct {
	Queued     func(ahead int) // no free slot: this many documents are ahead
	Reading    func()          // the turn came after waiting; reading the files
	Generating func()          // the text is read; making cards
	Cards      func(total int) // cards made so far
}

// Result is the saved set and what went wrong on single pages, such as a
// blurry photo.
type Result struct {
	Set      *contracts.CardSet
	Warnings []string
}

// Errors of Make; FailureText turns them into a message for the student.
var (
	ErrNoFiles = errors.New("no files to read")
	ErrGaveUp  = errors.New("gave up waiting for a turn")
	ErrNoText  = errors.New("no text in the files")
)

// NoCardsError is returned when the document gave no cards; Stats tell why.
type NoCardsError struct {
	Stats generator.Stats
}

func (e *NoCardsError) Error() string { return "no cards made from the document" }

// extractError is a failure to read the files, with their number: the
// message names the file at fault only when there are several.
type extractError struct {
	err   error
	files int
}

func (e *extractError) Error() string { return "read files: " + e.err.Error() }
func (e *extractError) Unwrap() error { return e.err }

// Make waits for a turn, reads the files, makes the cards and saves them as
// a set. The turn is held until the set is saved, so documents are saved in
// the order they came.
func (m *Maker) Make(ctx context.Context, req Request, p Progress) (Result, error) {
	if len(req.Files) == 0 {
		slog.Error("no files to read among attachments", "user_id", req.UserID)
		return Result{}, ErrNoFiles
	}
	title := resolveSetTitle(req.Caption, req.Files[0].Name)
	log := slog.With("user_id", req.UserID, "title", title)

	queued := false
	release, err := m.queue.Acquire(ctx, func(ahead int) {
		queued = true
		call(p.Queued, ahead)
	})
	if err != nil {
		log.Warn("stopped waiting for generation slot", "error", err)
		return Result{}, fmt.Errorf("%w: %w", ErrGaveUp, err)
	}
	defer release()
	if queued && p.Reading != nil {
		p.Reading()
	}

	extracted, err := m.extractor.Extract(ctx, req.Files)
	if err != nil {
		log.Error("failed to extract text from files", "error", err)
		return Result{}, &extractError{err: err, files: len(req.Files)}
	}
	if strings.TrimSpace(extracted.Text) == "" {
		log.Warn("no text found in files", "files_count", len(req.Files))
		return Result{}, ErrNoText
	}
	if p.Generating != nil {
		p.Generating()
	}

	doc := generator.Document{Text: extracted.Text, Title: title}
	for _, page := range extracted.Pages {
		doc.PageStarts = append(doc.PageStarts, page.Start)
	}
	var (
		mu    sync.Mutex
		total int
	)
	onBatch := func(b generator.Batch) {
		mu.Lock()
		total += len(b.Cards)
		n := total
		mu.Unlock()
		call(p.Cards, n)
	}
	genResult, err := m.generator.Generate(ctx, doc, onBatch)
	if err != nil {
		log.Error("failed to generate cards from document", "error", err)
		return Result{}, fmt.Errorf("generate cards: %w", err)
	}
	if len(genResult.Cards) == 0 {
		log.Warn("no cards generated from document", "stats", genResult.Stats)
		return Result{}, &NoCardsError{Stats: genResult.Stats}
	}

	if isGenericTitle(title) {
		if t := titleFromTopics(genResult.Facts); t != "" {
			title = t
		}
	}
	set, err := m.sets.SaveGeneratedSet(ctx, req.UserID, title, genResult)
	if err != nil {
		log.Error("failed to save generated set to database", "error", err)
		return Result{}, fmt.Errorf("save set: %w", err)
	}
	return Result{Set: set, Warnings: extracted.Warnings}, nil
}

func call(f func(int), n int) {
	if f != nil {
		f(n)
	}
}
