package perftest

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

const maxToken = "perf-max-token"
const telegramToken = "perf-telegram-token"

type actor struct {
	auth      string
	id        uuid.UUID
	answerMu  sync.Mutex
	index     int
	submitted atomic.Int64
}
type workload struct {
	server *httptest.Server
	client *http.Client
	actors []*actor
	owner  *actor
}

func identity(index int) *actor {
	scheme, token := "max", maxToken
	if index%2 == 1 {
		scheme, token = "tma", telegramToken
	}
	external := 10000000 + index/2
	if index < 0 {
		external = ownerExternalID
		scheme, token = "max", maxToken
	}
	user := fmt.Sprintf(`{"id":%d,"first_name":"Student","last_name":"Perf","username":"perf%d"}`, external, index)
	values := url.Values{"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)}, "user": {user}}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte("auth_date=" + values.Get("auth_date") + "\nuser=" + user))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return &actor{auth: scheme + " " + values.Encode(), index: index}
}
func (w *workload) request(ctx context.Context, r *recorder, a *actor, name, method, path string, body, out any) error {
	start := time.Now()
	err := w.do(ctx, a, method, path, body, out)
	if r != nil {
		r.add(name, time.Since(start), err)
	}
	return err
}
func (w *workload) do(ctx context.Context, a *actor, method, path string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, w.server.URL+"/api"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", a.auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %.240s", res.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
func (w *workload) entry(ctx context.Context, r *recorder, a *actor) error {
	var profile contracts.ProfileResponse
	if err := w.request(ctx, r, a, "profile", "GET", "/profile", nil, &profile); err != nil {
		return err
	}
	expected := "max"
	if a.index%2 == 1 {
		expected = "telegram"
	}
	if profile.User.ID == uuid.Nil || string(profile.Platform.Value) != expected {
		return fmt.Errorf("identity fell back to shared user: %+v", profile)
	}
	a.id = profile.User.ID
	if !profile.ProfileComplete {
		if err := w.request(ctx, r, a, "save_profile", "PUT", "/profile", map[string]string{"firstName": "Student", "lastName": "Perf"}, nil); err != nil {
			return err
		}
	}
	return w.request(ctx, r, a, "join", "POST", "/sets/join", map[string]string{"code": "700001"}, nil)
}
func (w *workload) burst(ctx context.Context, r *recorder, a *actor) error {
	// Readers load home, set and question-only feeds; full cards are author-only.
	paths := []struct{ name, path string }{{"home", "/"}, {"feed", "/feed"}, {"set", "/sets/" + setID(1)}, {"review_feed", "/feed?setId=" + setID(1) + "&early=true"}, {"feed", "/feed"}}
	errs := make(chan error, len(paths))
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Go(func() { errs <- w.request(ctx, r, a, p.name, "GET", p.path, nil, nil) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// answer reads the current question before grading it, just like the mini-app.
// Serialize one actor's submissions so concurrent scheduled actions do not submit
// different attempt IDs for the same question observed before a progress update.
func (w *workload) answer(ctx context.Context, r *recorder, a *actor) error {
	a.answerMu.Lock()
	defer a.answerMu.Unlock()
	var feed []contracts.FeedCard
	if err := w.request(ctx, r, a, "review_feed", "GET", "/feed?setId="+setID(1)+"&early=true", nil, &feed); err != nil {
		return err
	}
	if len(feed) == 0 {
		return fmt.Errorf("review feed is empty")
	}
	card := feed[0]
	answer, err := fixtureAnswer(card.Kind)
	if err != nil {
		return err
	}
	if card.Kind == contracts.CardKindFlip {
		if err = w.request(ctx, r, a, "reveal", "POST", "/cards/"+card.ID.String()+"/reveal", nil, nil); err != nil {
			return err
		}
	}
	var verdict contracts.AnswerQuestionResponse
	err = w.request(ctx, r, a, "answer", "POST", "/cards/"+card.ID.String()+"/answer", contracts.AnswerQuestionRequest{AttemptId: uuid.New(), Answer: answer}, &verdict)
	if err != nil {
		return err
	}
	if !verdict.IsCorrect || verdict.Method != contracts.AnswerQuestionResponseMethodLocal {
		err = fmt.Errorf("unexpected local verdict: %+v", verdict)
		if r != nil {
			r.add("answer_verdict", 0, err)
		}
		return err
	}
	a.submitted.Add(1)
	return nil
}

func fixtureAnswer(kind contracts.CardKind) (string, error) {
	switch kind {
	case contracts.CardKindFlip, contracts.CardKindBoolean:
		return "true", nil
	case contracts.CardKindChoice:
		return `["Answer"]`, nil
	case contracts.CardKindTable:
		return `[{"text":"Item","column":"A"}]`, nil
	case contracts.CardKindInput:
		return "Answer", nil
	default:
		return "", fmt.Errorf("unsupported fixture kind %q", kind)
	}
}

func (w *workload) leaderboard(ctx context.Context, r *recorder) error {
	return w.request(ctx, r, w.owner, "leaderboard", "GET", "/sets/"+setID(1)+"/leaderboard", nil, nil)
}

func (w *workload) study(ctx context.Context, r *recorder, d, think time.Duration) int64 {
	var completed atomic.Int64
	var wg sync.WaitGroup
	end := time.Now().Add(d)
	for _, a := range w.actors {
		wg.Go(func() {
			answers := 0
			ok := true
			for time.Now().Add(think).Before(end) {
				if !waitUntil(ctx, time.Now().Add(think)) {
					ok = false
					break
				}
				if err := w.answer(ctx, r, a); err != nil {
					ok = false
					break
				}
				answers++
				if answers%5 == 0 {
					if err := w.burst(ctx, r, a); err != nil {
						ok = false
						break
					}
				}
			}
			if ok {
				completed.Add(1)
			}
		})
	}
	wg.Go(func() {
		for time.Now().Add(15 * time.Second).Before(end) {
			if !waitUntil(ctx, time.Now().Add(15*time.Second)) {
				return
			}
			_ = w.leaderboard(ctx, r)
		}
	})
	wg.Wait()
	return completed.Load()
}
func (w *workload) rate(ctx context.Context, r *recorder, d time.Duration, rate int, deadline time.Duration) (int64, int64) {
	var missed, completed atomic.Int64
	var wg sync.WaitGroup
	// Bounded in-flight work: overload drops scheduled operations instead of
	// shifting their start times and hiding load-generator backpressure.
	slots := make(chan struct{}, len(w.actors)*3)
	start := time.Now()
	total := int(d.Seconds() * float64(rate))
	for i := 0; i < total; i++ {
		due := start.Add(time.Duration(float64(i) * float64(time.Second) / float64(rate)))
		if !waitUntil(ctx, due) {
			break
		}
		if time.Since(due) > deadline {
			missed.Add(1)
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			missed.Add(1)
			continue
		}
		a := w.actors[i%len(w.actors)]
		op := i % 10
		wg.Go(func() {
			defer func() { <-slots }()
			if time.Since(due) > deadline {
				missed.Add(1)
				return
			}
			var err error
			switch op {
			case 0:
				err = w.request(ctx, r, a, "home", "GET", "/", nil, nil)
			case 1, 2:
				err = w.request(ctx, r, a, "feed", "GET", "/feed", nil, nil)
			case 3:
				err = w.request(ctx, r, w.owner, "author_cards", "GET", "/sets/"+setID(1)+"/cards", nil, nil)
			case 4:
				err = w.request(ctx, r, a, "set", "GET", "/sets/"+setID(1), nil, nil)
			case 5, 6:
				err = w.request(ctx, r, a, "review_feed", "GET", "/feed?setId="+setID(1)+"&early=true", nil, nil)
			case 7, 8:
				err = w.answer(ctx, r, a)
			case 9:
				err = w.leaderboard(ctx, r)
			}
			if err == nil {
				completed.Add(1)
			}
		})
	}
	wg.Wait()
	return completed.Load(), missed.Load()
}

// A real static FS is required by the production router; API traffic never uses it.
var staticFS = fstest.MapFS{"index.html": {Data: []byte("perf")}}

func waitUntil(ctx context.Context, due time.Time) bool {
	timer := time.NewTimer(time.Until(due))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
