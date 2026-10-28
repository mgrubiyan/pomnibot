package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

const (
	photoImportTime = 15 * time.Second
	// A photo that failed for a passing reason, such as a slow CDN, is
	// tried again on a sign-in after this long.
	photoRetryAfter = time.Minute
	// A profile photo, even a full one; more is not a photo.
	maxPhotoDownload = 5 << 20
)

// PhotoImporter takes the user's photo from MAX: the signed
// launch data carries its address on every sign-in. The photo is fetched in
// the background and kept as the user's avatar, so the mini-app shows it
// from our own address and the messenger's never reaches anyone else.
type PhotoImporter struct {
	users  usecase.AvatarImporter
	client *http.Client

	mu sync.Mutex
	// What each user was last tried with: a photo being fetched or one that
	// cannot be read is not fetched again on every request.
	tried map[uuid.UUID]photoAttempt
	wg    sync.WaitGroup
	now   func() time.Time
}

type photoAttempt struct {
	url string
	// Zero while the photo is fetched and for one that will never do.
	retryAt time.Time
}

// errPhotoUnusable is a photo that will not work however often it is fetched.
var errPhotoUnusable = errors.New("messenger photo unusable")

// NewPhotoImporter fetches with client, or with a plain one under a timeout when it is nil.
func NewPhotoImporter(users usecase.AvatarImporter, client *http.Client) *PhotoImporter {
	if client == nil {
		client = &http.Client{Timeout: photoImportTime}
	}
	// Redirects stay on https, like the address the messenger signed.
	inner := *client
	inner.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 3 {
			return fmt.Errorf("%w: redirect refused", errPhotoUnusable)
		}
		return nil
	}
	return &PhotoImporter{users: users, client: &inner, tried: map[uuid.UUID]photoAttempt{}, now: time.Now}
}

// Sync starts bringing the avatar in step with photoURL, the address in the
// launch data, unless it already is or this address was already tried.
// The messenger photo also replaces one uploaded before there was no profile
// screen; with no messenger photo, an uploaded one stays.
func (p *PhotoImporter) Sync(ctx context.Context, account usecase.Account, photoURL string) {
	if p == nil || photoURL == account.AvatarSource {
		return
	}
	id := account.User.ID
	p.mu.Lock()
	if last, ok := p.tried[id]; ok && last.url == photoURL && (last.retryAt.IsZero() || p.now().Before(last.retryAt)) {
		p.mu.Unlock()
		return
	}
	p.tried[id] = photoAttempt{url: photoURL}
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), photoImportTime)
		defer cancel()
		err := p.sync(ctx, id, photoURL, account.AvatarSource != "")
		if err == nil {
			return
		}
		slog.WarnContext(ctx, "messenger photo not taken", "user_id", id, "error", err)
		if errors.Is(err, errPhotoUnusable) || errors.Is(err, usecase.ErrValidation) {
			return
		}
		p.mu.Lock()
		if last := p.tried[id]; last.url == photoURL {
			p.tried[id] = photoAttempt{url: photoURL, retryAt: p.now().Add(photoRetryAfter)}
		}
		p.mu.Unlock()
	}()
}

// Wait lets the photos being fetched finish; for tests and shutdown.
func (p *PhotoImporter) Wait() { p.wg.Wait() }

func (p *PhotoImporter) sync(ctx context.Context, id uuid.UUID, photoURL string, imported bool) error {
	if photoURL == "" {
		if !imported {
			return nil
		}
		return p.users.ForgetImportedAvatar(ctx, id)
	}
	u, err := url.Parse(photoURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: address %q is not https", errPhotoUnusable, photoURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch photo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("%w: %s", errPhotoUnusable, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch photo: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoDownload+1))
	if err != nil {
		return fmt.Errorf("read photo: %w", err)
	}
	if len(data) > maxPhotoDownload {
		return fmt.Errorf("%w: over 5 MB", errPhotoUnusable)
	}
	_, err = p.users.ImportAvatar(ctx, id, photoURL, data)
	return err
}
