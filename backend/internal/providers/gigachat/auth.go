package gigachat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// tokenTTL is the documented lifetime of an access token, used when the
	// OAuth answer has no expires_at.
	tokenTTL = 30 * time.Minute
	// refreshBefore is how long before expiry the token is replaced, so no
	// request ever goes out with a token about to expire.
	refreshBefore = 5 * time.Minute
	// tokenMinValid: a token closer to expiry than this is not handed out.
	tokenMinValid = 30 * time.Second
	// refreshRetry is the pause after a failed background refresh. The OAuth
	// endpoint allows about 10 requests per second; one refresher per client
	// stays far below that.
	refreshRetry = 30 * time.Second
	authTimeout  = 30 * time.Second
)

// nextRefresh is when to replace a token that has ttl left: refreshBefore
// ahead of expiry, but not before half its life, so a short-lived token (or a
// skewed clock) does not turn into a refresh every second.
func nextRefresh(ttl, before time.Duration) time.Duration {
	return max(ttl-before, ttl/2, time.Second)
}

// tokenSource exchanges client credentials for an access token and keeps it
// fresh. The token is replaced by a timer ahead of expiry rather than after
// a 401: a request never waits for a refresh unless the timer failed.
type tokenSource struct {
	http          *http.Client
	authURL       string
	scope         string
	basic         string
	refreshBefore time.Duration
	minValid      time.Duration // tokenMinValid, a field for tests

	mu        sync.RWMutex
	token     string
	expiresAt time.Time

	fetchMu sync.Mutex // one exchange at a time
	timer   *time.Timer
	closed  bool
}

// newTokenSource takes the Authorization Key, base64 of client_id:client_secret.
func newTokenSource(hc *http.Client, authURL, scope, authKey string) *tokenSource {
	return &tokenSource{
		http:          hc,
		authURL:       authURL,
		scope:         scope,
		basic:         authKey,
		refreshBefore: refreshBefore,
		minValid:      tokenMinValid,
	}
}

// Token returns a valid access token, fetching one only if none is cached.
func (s *tokenSource) Token(ctx context.Context) (string, error) {
	if tok, ok := s.cached(); ok {
		return tok, nil
	}
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	if tok, ok := s.cached(); ok { // refreshed while we waited
		return tok, nil
	}
	return s.refreshLocked(ctx)
}

func (s *tokenSource) cached() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token, s.token != "" && time.Until(s.expiresAt) > s.minValid
}

// refreshLocked fetches a token and schedules the next refresh. fetchMu must
// be held.
func (s *tokenSource) refreshLocked(ctx context.Context) (string, error) {
	tok, exp, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.token, s.expiresAt = tok, exp
	s.mu.Unlock()
	s.schedule(nextRefresh(time.Until(exp), s.refreshBefore))
	return tok, nil
}

func (s *tokenSource) schedule(after time.Duration) {
	if s.closed {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(max(after, time.Second), s.backgroundRefresh)
}

func (s *tokenSource) backgroundRefresh() {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	if s.closed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), authTimeout)
	defer cancel()
	if _, err := s.refreshLocked(ctx); err != nil {
		s.mu.RLock()
		left := time.Until(s.expiresAt)
		s.mu.RUnlock()
		// Retry while the current token is still good. Once it has run out,
		// stop: an idle client should not poll OAuth forever, and the next
		// request fetches a token on demand.
		if left > s.minValid {
			retry := min(refreshRetry, left/2)
			slog.Warn("gigachat: background token refresh failed", "err", err, "retry_in", retry)
			s.schedule(retry)
			return
		}
		slog.Warn("gigachat: background token refresh failed, next request will fetch a token", "err", err)
	}
}

// Close stops background refreshing.
func (s *tokenSource) Close() {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"` // Unix milliseconds
}

func (s *tokenSource) fetch(ctx context.Context) (string, time.Time, error) {
	form := url.Values{"scope": {s.scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gigachat: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+s.basic)
	req.Header.Set("RqUID", newRqUID())

	resp, err := s.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gigachat: token request: %w", transportError{err})
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gigachat: read token response: %w", transportError{err})
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, &APIError{
			Status:     resp.StatusCode,
			Body:       clip(body),
			RetryAfter: retryAfter(resp.Header),
			op:         opToken,
		}
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", time.Time{}, fmt.Errorf("gigachat: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("gigachat: token response has no access_token")
	}
	// Trust the lifetime, not the server clock: counted from receipt and
	// capped at the documented 30 minutes. With a skewed clock expires_at may
	// look past already, and every request would then fetch a new token.
	ttl := tokenTTL
	if tr.ExpiresAt > 0 {
		if left := time.Until(time.UnixMilli(tr.ExpiresAt)); left > s.minValid {
			ttl = min(left, tokenTTL)
		}
	}
	exp := time.Now().Add(ttl)
	slog.Debug("gigachat: access token received", "expires_at", exp)
	return tr.AccessToken, exp, nil
}

// newRqUID returns a random UUID v4, which the OAuth endpoint requires to
// trace the request.
func newRqUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails on supported platforms
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
