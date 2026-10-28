package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
)

// MaxNotesBytes is how much a document from the mini app may weigh, all its
// files together.
const MaxNotesBytes = 20 << 20

// maxNotesRequest fits MaxNotesBytes of files and their multipart headers.
const maxNotesRequest = MaxNotesBytes + 64<<10

// notesUploadTime is how long a phone may take to send a document. The
// server's timeouts are for every other request.
const notesUploadTime = 5 * time.Minute

// notesKept is how long a done document's status is kept for the mini app.
const notesKept = time.Hour

// NoteMaker makes a card set from the files of a document; notes.Maker is
// one, shared with the bots.
type NoteMaker interface {
	Make(ctx context.Context, req notes.Request, p notes.Progress) (notes.Result, error)
}

// WithNotes lets the mini app send notes. The sets are made under ctx, not
// under the request that brought the files: it ends as soon as they are
// taken.
func (h *APIHandler) WithNotes(ctx context.Context, maker NoteMaker) *APIHandler {
	h.notes = &noteJobs{ctx: ctx, maker: maker, now: time.Now, jobs: make(map[uuid.UUID]*noteJob)}
	return h
}

// WaitNotes takes no more documents and waits for those being made to stop,
// once the context of WithNotes is done. An upload may still be arriving
// when the server gives up waiting for it on shutdown.
func (h *APIHandler) WaitNotes() {
	if h.notes == nil {
		return
	}
	h.notes.mu.Lock()
	h.notes.closed = true
	h.notes.mu.Unlock()
	h.notes.wg.Wait()
}

// noteJobs keeps the latest document of each user in memory: production
// runs in one container, and after a restart a made set is among the user's
// sets anyway.
type noteJobs struct {
	ctx   context.Context
	maker NoteMaker
	now   func() time.Time
	wg    sync.WaitGroup

	mu     sync.Mutex
	jobs   map[uuid.UUID]*noteJob
	closed bool // WaitNotes has begun: no wg.Add after it
}

type noteJob struct {
	status contracts.NotesJob
	done   time.Time // zero while the document is made
}

var (
	errNotesBusy   = errors.New("previous document is still being made")
	errNotesClosed = errors.New("the server is shutting down")
)

// start makes the set in the background, one document of a user at a time.
func (j *noteJobs) start(userID uuid.UUID, files []ingest.File) (contracts.NotesJob, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.forget()
	if j.closed || j.ctx.Err() != nil {
		return contracts.NotesJob{}, errNotesClosed
	}
	if job, ok := j.jobs[userID]; ok && job.done.IsZero() {
		return contracts.NotesJob{}, errNotesBusy
	}
	job := &noteJob{status: contracts.NotesJob{State: contracts.NotesJobStateReading}}
	j.jobs[userID] = job
	j.wg.Add(1)
	go func() {
		defer j.wg.Done()
		res, err := j.maker.Make(j.ctx, notes.Request{UserID: userID, Files: files}, notes.Progress{
			Queued: func(ahead int) {
				j.set(job, contracts.NotesJob{State: contracts.NotesJobStateQueued, Ahead: contracts.NewOptInt(ahead)})
			},
			Reading: func() { j.set(job, contracts.NotesJob{State: contracts.NotesJobStateReading}) },
			Generating: func() {
				j.set(job, contracts.NotesJob{State: contracts.NotesJobStateGenerating, Cards: contracts.NewOptInt(0)})
			},
			Cards: func(total int) {
				j.set(job, contracts.NotesJob{State: contracts.NotesJobStateGenerating, Cards: contracts.NewOptInt(total)})
			},
		})
		j.mu.Lock()
		defer j.mu.Unlock()
		job.status, job.done = finished(res, err), j.now()
	}()
	return job.status, nil
}

func (j *noteJobs) set(job *noteJob, status contracts.NotesJob) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job.done.IsZero() {
		job.status = status
	}
}

func finished(res notes.Result, err error) contracts.NotesJob {
	if err == nil {
		return contracts.NotesJob{State: contracts.NotesJobStateDone, SetId: contracts.NewOptUUID(res.Set.ID), Remarks: contracts.NewOptBool(len(res.Warnings) > 0)}
	}
	reason, message := notes.Failure(err)
	// The reasons the mini app never meets, such as giving up on shutdown,
	// read as our own failure.
	r := contracts.NotesJobReason(reason)
	if r.Validate() != nil {
		r = contracts.NotesJobReasonTryLater
	}
	status := contracts.NotesJob{State: contracts.NotesJobStateFailed, Reason: contracts.NewOptNotesJobReason(r)}
	if message != "" {
		status.Message = contracts.NewOptString(message)
	}
	return status
}

func (j *noteJobs) status(userID uuid.UUID) (contracts.NotesJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.forget()
	job, ok := j.jobs[userID]
	if !ok {
		return contracts.NotesJob{}, false
	}
	return job.status, true
}

// forget drops the documents done over notesKept ago. The caller holds mu.
func (j *noteJobs) forget() {
	now := j.now()
	for id, job := range j.jobs {
		if !job.done.IsZero() && now.Sub(job.done) > notesKept {
			delete(j.jobs, id)
		}
	}
}

// UploadNotes takes the files of a document and makes its set in the
// background. Only for a verified account: in production a request without
// a signature is the shared account, and every document costs model money.
func (h *APIHandler) UploadNotes(ctx context.Context, req *contracts.NotesUploadMultipart) (contracts.UploadNotesRes, error) {
	if h.notes == nil {
		return &contracts.UploadNotesServiceUnavailable{Message: "making cards from notes is off"}, nil
	}
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.UploadNotesInternalServerError{Message: internalError(ctx, "UploadNotes", err)}, nil
	}
	if !ok {
		return &contracts.UploadNotesForbidden{Message: "authorization required"}, nil
	}
	if len(req.Files) == 0 {
		return &contracts.UploadNotesBadRequest{Message: "no files"}, nil
	}
	var size int64
	for _, f := range req.Files {
		size += f.Size
	}
	if size > MaxNotesBytes {
		return &contracts.UploadNotesRequestEntityTooLarge{Message: "files over 20 MB in all"}, nil
	}
	files := make([]ingest.File, 0, len(req.Files))
	for _, f := range req.Files {
		data, err := io.ReadAll(f.File)
		if err != nil {
			return &contracts.UploadNotesBadRequest{Message: fmt.Sprintf("read %q: %v", f.Name, err)}, nil
		}
		if len(data) == 0 {
			return &contracts.UploadNotesBadRequest{Message: fmt.Sprintf("file %q is empty", f.Name)}, nil
		}
		name := f.Name
		if name == "" {
			name = "document"
		}
		files = append(files, ingest.File{Name: name, Data: data})
	}
	status, err := h.notes.start(account.User.ID, files)
	if errors.Is(err, errNotesBusy) {
		return &contracts.UploadNotesConflict{Message: err.Error()}, nil
	}
	if errors.Is(err, errNotesClosed) {
		return &contracts.UploadNotesServiceUnavailable{Message: err.Error()}, nil
	}
	return &status, nil
}

// GetNotesJob tells how the user's latest document goes.
func (h *APIHandler) GetNotesJob(ctx context.Context) (contracts.GetNotesJobRes, error) {
	if h.notes == nil {
		return &contracts.GetNotesJobServiceUnavailable{Message: "making cards from notes is off"}, nil
	}
	account, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.GetNotesJobInternalServerError{Message: internalError(ctx, "GetNotesJob", err)}, nil
	}
	if !ok {
		return &contracts.GetNotesJobForbidden{Message: "authorization required"}, nil
	}
	status, ok := h.notes.status(account.User.ID)
	if !ok {
		return &contracts.GetNotesJobNotFound{Message: "no document lately"}, nil
	}
	return &status, nil
}
