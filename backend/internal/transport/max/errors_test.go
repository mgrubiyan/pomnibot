package max

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
)

func (s *recordingServer) lastText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.texts) == 0 {
		return ""
	}
	return s.texts[len(s.texts)-1]
}

func TestBot_FailuresTellTheStudentWhatHappened(t *testing.T) {
	failingGen := func(err error, stats generator.Stats) *mockCardGenerator {
		return &mockCardGenerator{generateFunc: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
			return generator.Result{Stats: stats}, err
		}}
	}
	extractErr := func(err error) *mockFileExtractor {
		return &mockFileExtractor{extractFunc: func(context.Context, []ingest.File) (ingest.Result, error) {
			return ingest.Result{}, err
		}}
	}

	tests := []struct {
		name      string
		update    func(serverURL string) Update
		extractor *mockFileExtractor
		gen       *mockCardGenerator
		saveErr   error
		want      string
	}{
		{
			name: "download fails",
			update: func(serverURL string) Update {
				u := fileUpdate(1, "notes.pdf")
				u.Message.Body.Attachments[0].Payload = map[string]interface{}{
					"name": "notes.pdf",
					"url":  serverURL + "/download/missing",
				}
				return u
			},
			want: notes.TextResend,
		},
		{
			name: "unsupported format",
			extractor: extractErr(&ingest.Error{File: "notes.docx", Err: ingest.ErrUnsupported,
				Message: "Формат не поддерживается: пришлите текст, PDF или фото страниц."}),
			want: "Формат не поддерживается: пришлите текст, PDF или фото страниц.",
		},
		{
			name: "pdf over the page limit",
			extractor: extractErr(&ingest.Error{File: "notes.pdf", Err: ingest.ErrTooManyPages,
				Message: "В PDF 300 страниц, а распознать можно не больше 200. Разбейте файл на части."}),
			want: "В PDF 300 страниц",
		},
		{
			name:      "extraction fails for an unknown reason",
			extractor: extractErr(errors.New("boom")),
			want:      notes.TextTryLater,
		},
		{
			name: "no text in the files",
			extractor: &mockFileExtractor{extractFunc: func(context.Context, []ingest.File) (ingest.Result, error) {
				return ingest.Result{Text: "  \n "}, nil
			}},
			want: notes.TextNoText,
		},
		{
			name: "model fails",
			gen:  failingGen(fmt.Errorf("provider failed on 3 fragments in a row: %w", errors.New("503")), generator.Stats{}),
			want: notes.TextTryLater,
		},
		{
			name: "only a table of contents",
			gen:  failingGen(nil, generator.Stats{Chunks: 2, ChunksFiltered: 2}),
			want: notes.TextOnlyJunk,
		},
		{
			name: "content filter refused every fragment",
			gen:  failingGen(nil, generator.Stats{Chunks: 3, ChunksFiltered: 1, ChunksRefused: 2}),
			want: notes.TextOtherText,
		},
		{
			name: "no card backed by a quote",
			gen:  failingGen(nil, generator.Stats{Chunks: 2, CardsFromModel: 6, DroppedQuote: 6}),
			want: notes.TextNoCards,
		},
		{
			name:    "saving fails",
			saveErr: errors.New("db down"),
			want:    notes.TextTryLater,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newRecordingServer(t)
			client, err := NewClient("test-token", server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			extractor := tt.extractor
			if extractor == nil {
				extractor = &mockFileExtractor{}
			}
			gen := tt.gen
			if gen == nil {
				gen = &mockCardGenerator{}
			}
			setSvc := &mockSetService{saveGeneratedSetFunc: func(_ context.Context, _ uuid.UUID, title string, _ generator.Result) (*contracts.CardSet, error) {
				if tt.saveErr != nil {
					return nil, tt.saveErr
				}
				return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
			}}
			b, err := NewBot(client, "https://example.test", &mockUserService{}, notes.New(extractor, gen, setSvc, 1))
			if err != nil {
				t.Fatalf("NewBot: %v", err)
			}

			u := fileUpdate(1, "notes.pdf")
			if tt.update != nil {
				u = tt.update(server.URL)
			}
			b.handleUpdate(context.Background(), u)

			got := server.lastText()
			if !strings.Contains(got, tt.want) {
				t.Errorf("last message = %q, want it to contain %q", got, tt.want)
			}
			if strings.Contains(got, "Попробуйте еще раз позже") {
				t.Errorf("last message is the generic error: %q", got)
			}
			// The student cannot do anything about our insides.
			for _, inside := range []string{"сервис", "сохран", "цитат", "GigaChat"} {
				if strings.Contains(strings.ToLower(got), strings.ToLower(inside)) {
					t.Errorf("last message talks about %q: %q", inside, got)
				}
			}
		})
	}
}

func TestBot_SuccessMentionsPoorlyReadPages(t *testing.T) {
	server := newRecordingServer(t)
	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	extractor := &mockFileExtractor{extractFunc: func(context.Context, []ingest.File) (ingest.Result, error) {
		return ingest.Result{
			Text:     "Митоз состоит из четырёх фаз.",
			Pages:    []ingest.PageInfo{{Number: 1}},
			Warnings: []string{"Страница 1 распознана плохо, попробуйте переснять."},
		}, nil
	}}
	setSvc := &mockSetService{saveGeneratedSetFunc: func(_ context.Context, _ uuid.UUID, title string, _ generator.Result) (*contracts.CardSet, error) {
		return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
	}}
	b, err := NewBot(client, "https://example.test", &mockUserService{}, notes.New(extractor, &mockCardGenerator{}, setSvc, 1))
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}

	b.handleUpdate(context.Background(), fileUpdate(1, "photo.jpg"))

	got := server.lastText()
	if !strings.Contains(got, "успешно создан") || !strings.Contains(got, "Страница 1 распознана плохо") {
		t.Errorf("last message = %q, want the success text with the page warning", got)
	}
}

func TestBot_UndeliveredStatusIsLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// MAX accepts nothing: every send and edit fails.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	gen := &mockCardGenerator{generateFunc: func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
		return generator.Result{}, errors.New("model down")
	}}
	b, err := NewBot(client, "https://example.test", &mockUserService{}, notes.New(&mockFileExtractor{}, gen, &mockSetService{}, 1))
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}

	b.handleUpdate(context.Background(), fileUpdate(7, "notes.pdf"))

	got := logs.String()
	if !strings.Contains(got, "failed to send status message") || !strings.Contains(got, "user_id=7") {
		t.Errorf("an undelivered status is not in the log: %q", got)
	}
}
