package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// fakeMaker hands every document to the test, which plays the queue and the
// model through the call's progress and answers it on done.
type fakeMaker struct {
	calls chan *makeCall
}

type makeCall struct {
	ctx  context.Context
	req  notes.Request
	p    notes.Progress
	done chan makeResult
}

type makeResult struct {
	res notes.Result
	err error
}

func newFakeMaker() *fakeMaker { return &fakeMaker{calls: make(chan *makeCall)} }

func (f *fakeMaker) Make(ctx context.Context, req notes.Request, p notes.Progress) (notes.Result, error) {
	c := &makeCall{ctx: ctx, req: req, p: p, done: make(chan makeResult)}
	f.calls <- c
	r := <-c.done
	return r.res, r.err
}

func (f *fakeMaker) next(t *testing.T) *makeCall {
	t.Helper()
	select {
	case c := <-f.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Error("the document never reached the maker")
		return nil
	}
}

type notesFile struct{ name, data string }

func notesBody(t *testing.T, files ...notesFile) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := w.CreateFormFile("files", f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

type notesServer struct {
	t      *testing.T
	router http.Handler
	auth   string
}

func newNotesServer(t *testing.T, handler *APIHandler, users usecase.UserService) notesServer {
	t.Helper()
	router, err := NewRouter(handler, fstest.MapFS{}, "secret", "", users, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := generateValidInitData(map[string]string{"auth_date": fmt.Sprint(time.Now().Unix()), "user": `{"id":42}`}, "secret")
	return notesServer{t: t, router: router, auth: "max " + raw}
}

func (s notesServer) upload(ctx context.Context, auth string, files ...notesFile) *httptest.ResponseRecorder {
	s.t.Helper()
	body, contentType := notesBody(s.t, files...)
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/notes", body)
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	return w
}

func (s notesServer) status(auth string) (int, map[string]any) {
	s.t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	r.Header.Set("Authorization", auth)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	var job map[string]any
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
			s.t.Fatalf("status body %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, job
}

// waitState polls the status until it says want, as the mini app does.
func (s notesServer) waitState(want map[string]any) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var job map[string]any
	for time.Now().Before(deadline) {
		var code int
		code, job = s.status(s.auth)
		if code == http.StatusOK && fmt.Sprint(job) == fmt.Sprint(want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	s.t.Fatalf("status = %v, want %v", job, want)
}

func notesUsers() *profileUsers {
	return &profileUsers{account: usecase.Account{User: contracts.User{ID: uuid.MustParse("00000000-0000-0000-0000-000000000905"), FirstName: "Ann"}, MaxID: 42, ProfileComplete: true}}
}

func TestUploadNotesMakesTheSetInTheBackground(t *testing.T) {
	users := notesUsers()
	maker := newFakeMaker()
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), maker), users)

	// The request ends as soon as the files are taken: the set is made
	// after it, so its context must not stop the work.
	requestCtx, endRequest := context.WithCancel(context.Background())
	w := server.upload(requestCtx, server.auth, notesFile{"page1.jpg", "first"}, notesFile{"page2.jpg", "second"})
	endRequest()
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"state":"reading"`) {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	call := maker.next(t)
	if call.req.UserID != users.account.User.ID {
		t.Errorf("made for %s, want %s", call.req.UserID, users.account.User.ID)
	}
	if want := []ingest.File{{Name: "page1.jpg", Data: []byte("first")}, {Name: "page2.jpg", Data: []byte("second")}}; fmt.Sprint(call.req.Files) != fmt.Sprint(want) {
		t.Errorf("files = %v, want %v", call.req.Files, want)
	}
	if call.ctx.Err() != nil {
		t.Fatalf("the document stops with its request: %v", call.ctx.Err())
	}

	if w := server.upload(context.Background(), server.auth, notesFile{"other.pdf", "x"}); w.Code != http.StatusConflict {
		t.Fatalf("second document while the first is made: %d %s", w.Code, w.Body.String())
	}

	call.p.Queued(2)
	server.waitState(map[string]any{"state": "queued", "ahead": 2})
	call.p.Reading()
	server.waitState(map[string]any{"state": "reading"})
	call.p.Generating()
	server.waitState(map[string]any{"state": "generating", "cards": 0})
	call.p.Cards(7)
	server.waitState(map[string]any{"state": "generating", "cards": 7})

	setID := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	call.done <- makeResult{res: notes.Result{Set: &contracts.CardSet{ID: setID}, Warnings: []string{"Страница 2 размыта"}}}
	server.waitState(map[string]any{"state": "done", "setId": setID.String(), "remarks": true})

	// Once done, the next document may come.
	if w := server.upload(context.Background(), server.auth, notesFile{"again.txt", "text"}); w.Code != http.StatusAccepted {
		t.Fatalf("upload after done: %d %s", w.Code, w.Body.String())
	}
	maker.next(t).done <- makeResult{err: errors.New("database down")}
	server.waitState(map[string]any{"state": "failed", "reason": "try_later"})
	if w := server.upload(context.Background(), server.auth, notesFile{"again.txt", "text"}); w.Code != http.StatusAccepted {
		t.Fatalf("upload after failure: %d %s", w.Code, w.Body.String())
	}
	maker.next(t).done <- makeResult{err: fmt.Errorf("generate cards: %w", notes.ErrNoCards)}
	server.waitState(map[string]any{"state": "failed", "reason": "no_cards"})
}

// unreadable reads no file; the rest of the Maker is never reached.
type unreadable struct{ err error }

func (u unreadable) Extract(context.Context, []ingest.File) (ingest.Result, error) {
	return ingest.Result{}, u.err
}

type unusedGenerator struct{}

func (unusedGenerator) Generate(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
	return generator.Result{}, errors.New("unused")
}

type unusedSaver struct{}

func (unusedSaver) SaveGeneratedSet(context.Context, uuid.UUID, string, generator.Result, []string) (*contracts.CardSet, error) {
	return nil, errors.New("unused")
}

func TestUploadNotesTellsWhatIsWrongWithTheFiles(t *testing.T) {
	users := notesUsers()
	message := "Не удалось открыть документ Word: сохраните его заново или пришлите PDF."
	maker := notes.New(unreadable{&ingest.Error{File: "lecture.docx", Message: message, Err: ingest.ErrUnsupported}}, unusedGenerator{}, unusedSaver{}, 1)
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), maker), users)

	if w := server.upload(context.Background(), server.auth, notesFile{"lecture.docx", "PK"}); w.Code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	server.waitState(map[string]any{"state": "failed", "reason": "unreadable", "message": message})
	if w := server.upload(context.Background(), server.auth, notesFile{"lecture.docx", "PK"}, notesFile{"slides.pdf", "%PDF"}); w.Code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	server.waitState(map[string]any{"state": "failed", "reason": "unreadable", "message": "Файл «lecture.docx»: " + message})
}

func TestNotesJobIsForgottenAnHourAfterItIsDone(t *testing.T) {
	users := notesUsers()
	maker := newFakeMaker()
	handler := NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), maker)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	var clock = func() time.Time { return now }
	handler.notes.now = func() time.Time { return clock() }
	server := newNotesServer(t, handler, users)

	if code, _ := server.status(server.auth); code != http.StatusNotFound {
		t.Fatalf("status before any document: %d", code)
	}
	if w := server.upload(context.Background(), server.auth, notesFile{"a.txt", "text"}); w.Code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	setID := uuid.New()
	maker.next(t).done <- makeResult{res: notes.Result{Set: &contracts.CardSet{ID: setID}}}
	server.waitState(map[string]any{"state": "done", "setId": setID.String(), "remarks": false})

	handler.notes.mu.Lock()
	now = now.Add(59 * time.Minute)
	handler.notes.mu.Unlock()
	if code, _ := server.status(server.auth); code != http.StatusOK {
		t.Fatalf("status within the hour: %d", code)
	}
	handler.notes.mu.Lock()
	now = now.Add(2 * time.Minute)
	handler.notes.mu.Unlock()
	if code, _ := server.status(server.auth); code != http.StatusNotFound {
		t.Fatalf("status after the hour: %d", code)
	}
}

func TestUploadNotesRefusals(t *testing.T) {
	users := notesUsers()
	maker := newFakeMaker()
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), maker), users)
	file := notesFile{"a.txt", "text"}

	// Without a signature the request is the shared account anyone may use
	// in production, and every document costs model money.
	if w := server.upload(context.Background(), "", file); w.Code != http.StatusForbidden {
		t.Fatalf("unsigned upload: %d %s", w.Code, w.Body.String())
	}
	if code, _ := server.status(""); code != http.StatusForbidden {
		t.Fatalf("unsigned status: %d", code)
	}
	if w := server.upload(context.Background(), server.auth); w.Code != http.StatusBadRequest {
		t.Fatalf("no files: %d %s", w.Code, w.Body.String())
	}
	if w := server.upload(context.Background(), server.auth, notesFile{"empty.txt", ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty file: %d %s", w.Code, w.Body.String())
	}
	big := strings.Repeat("x", MaxNotesBytes/2+1)
	if w := server.upload(context.Background(), server.auth, notesFile{"a.pdf", big}, notesFile{"b.pdf", big}); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large: %d %s", w.Code, w.Body.String())
	}

	// A local server has no models.
	off := newNotesServer(t, NewAPIHandler(nil, nil, nil, users), users)
	if w := off.upload(context.Background(), off.auth, file); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("upload without models: %d %s", w.Code, w.Body.String())
	}
	if code, _ := off.status(off.auth); code != http.StatusServiceUnavailable {
		t.Fatalf("status without models: %d", code)
	}
}

// A phone may take a minute to send 20 MB; the server's timeouts are for
// every other request.
func TestUploadNotesOutlastsTheServerTimeouts(t *testing.T) {
	users := notesUsers()
	maker := newFakeMaker()
	ns := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), maker), users)
	server := httptest.NewUnstartedServer(ns.router)
	server.Config.ReadTimeout = 100 * time.Millisecond
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()

	body, contentType := notesBody(t, notesFile{"slow.txt", "slow notes"})
	reader, writer := io.Pipe()
	go func() {
		data := body.Bytes()
		step := len(data)/4 + 1
		for i := 0; i < len(data); i += step {
			time.Sleep(100 * time.Millisecond)
			if _, err := writer.Write(data[i:min(i+step, len(data))]); err != nil {
				return
			}
		}
		_ = writer.Close()
	}()
	r, err := http.NewRequest(http.MethodPost, server.URL+"/api/notes", reader)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Authorization", ns.auth)
	go func() {
		if c := maker.next(t); c != nil {
			c.done <- makeResult{err: errors.New("stop")}
		}
	}()
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("slow upload cut off: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("slow upload: %d %s", resp.StatusCode, data)
	}
}

func TestUploadNotesAfterShutdownBegins(t *testing.T) {
	users := notesUsers()
	maker := newFakeMaker()
	ctx, cancel := context.WithCancel(context.Background())
	handler := NewAPIHandler(nil, nil, nil, users).WithNotes(ctx, maker)
	server := newNotesServer(t, handler, users)

	// An upload still arriving when the server stops waiting for it must not
	// start a document the shutdown no longer waits for.
	cancel()
	handler.WaitNotes()
	if w := server.upload(context.Background(), server.auth, notesFile{"late.txt", "text"}); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("upload after shutdown: %d %s", w.Code, w.Body.String())
	}
	select {
	case <-maker.calls:
		t.Fatal("a document started after shutdown")
	default:
	}
}

// An unsigned upload is refused before its body is read.
func TestUnsignedUploadIsRefusedUnread(t *testing.T) {
	users := notesUsers()
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithNotes(context.Background(), newFakeMaker()), users)
	body := &countingReader{r: strings.NewReader(strings.Repeat("x", 1<<20))}
	r := httptest.NewRequest(http.MethodPost, "/api/notes", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || body.n != 0 {
		t.Fatalf("unsigned upload: %d, read %d bytes", w.Code, body.n)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}
