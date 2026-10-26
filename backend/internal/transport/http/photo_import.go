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
	// A profile photo, even a full one; more is not a photo.
	maxPhotoDownload = 5 << 20
)

// PhotoImporter takes the user's photo from MAX or Telegram: the signed
// launch data carries its address on every sign-in. The photo is fetched in
// the background and kept as the user's avatar, so the mini-app shows it
// from our own address and the messenger's never reaches anyone else.
type PhotoImporter struct {
	users  usecase.AvatarImporter
	client *http.Client

	mu sync.Mutex
	// What each user was last tried with, whether it worked or not: a photo
	// that cannot be read is not fetched again on every request.
	tried map[uuid.UUID]string
	wg    sync.WaitGroup
}

// NewPhotoImporter fetches with client, or with a plain one under a timeout when it is nil.
func NewPhotoImporter(users usecase.AvatarImporter, client *http.Client) *PhotoImporter {
	if client == nil {
		client = &http.Client{Timeout: photoImportTime}
	}
	// Redirects stay on https, like the address the messenger signed.
	inner := *client
	inner.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 3 {
			return errors.New("photo redirect refused")
		}
		return nil
	}
	return &PhotoImporter{users: users, client: &inner, tried: map[uuid.UUID]string{}}
}

// Sync starts bringing the avatar in step with photoURL, the address in the
// launch data, unless it already is or this address was already tried.
func (p *PhotoImporter) Sync(ctx context.Context, account usecase.Account, photoURL string) {
	if p == nil || photoURL == account.AvatarSource {
		return
	}
	id := account.User.ID
	p.mu.Lock()
	if last, ok := p.tried[id]; ok && last == photoURL {
		p.mu.Unlock()
		return
	}
	p.tried[id] = photoURL
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), photoImportTime)
		defer cancel()
		if err := p.sync(ctx, id, photoURL, account.AvatarSource != ""); err != nil {
			slog.WarnContext(ctx, "messenger photo not taken", "user_id", id, "error", err)
		}
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
		return fmt.Errorf("photo address %q is not https", photoURL)
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch photo: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoDownload+1))
	if err != nil {
		return fmt.Errorf("read photo: %w", err)
	}
	if len(data) > maxPhotoDownload {
		return errors.New("photo over 5 MB")
	}
	_, err = p.users.ImportAvatar(ctx, id, photoURL, data)
	return err
}
