//go:build perf

package main

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

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

const maxToken = "perf-max-token"
const telegramToken = "perf-telegram-token"

type actor struct {
	auth      string
	id        int64
	index     int
	submitted atomic.Int64
	batches   atomic.Int64
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
	if profile.User.ID == 0 || string(profile.Platform.Value) != expected {
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
	// Home.tsx: today+feed. SetScreen.tsx: set+cards+feed.
	paths := []struct{ name, path string }{{"home", "/"}, {"feed", "/feed"}, {"set", "/sets/" + setID(1)}, {"cards", "/sets/" + setID(1) + "/cards"}, {"feed", "/feed"}}
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
func (w *workload) check(ctx context.Context, r *recorder, a *actor, answer int) error {
	var v contracts.CheckAnswerResponse
	err := w.request(ctx, r, a, "check", "POST", "/cards/"+cardID(answer%50+1, 4)+"/check", map[string]string{"answer": "Answer"}, &v)
	if err == nil && (!v.IsCorrect || v.Method != contracts.CheckAnswerResponseMethodLocal) {
		err = fmt.Errorf("unexpected local verdict: %+v", v)
		if r != nil {
			r.add("check_verdict", 0, err)
		}
	}
	return err
}
func (w *workload) submit(ctx context.Context, r *recorder, a *actor, first, count int) error {
	results := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		results = append(results, map[string]any{"cardId": cardID((first+i)%50+1, (first+i)%5), "correct": true, "answeredAt": time.Now().UTC()})
	}
	err := w.request(ctx, r, a, "results", "POST", "/results", results, nil)
	if err == nil {
		a.submitted.Add(int64(count))
		a.batches.Add(1)
	}
	return err
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
			answer, pending := 0, 0
			ok := true
			for time.Now().Add(think).Before(end) {
				time.Sleep(think)
				// Typed cards use the check endpoint; other kinds are graded in the browser.
				if answer%5 == 4 {
					if err := w.check(ctx, r, a, answer); err != nil {
						ok = false
						break
					}
				}
				answer++
				pending++
				if pending == 5 {
					if err := w.submit(ctx, r, a, answer-pending, pending); err != nil {
						ok = false
						break
					}
					pending = 0
					if err := w.burst(ctx, r, a); err != nil {
						ok = false
						break
					}
				}
			}
			if pending > 0 {
				if err := w.submit(ctx, r, a, answer-pending, pending); err != nil {
					ok = false
				}
			}
			if ok {
				completed.Add(1)
			}
		})
	}
	wg.Go(func() {
		for time.Now().Add(15 * time.Second).Before(end) {
			time.Sleep(15 * time.Second)
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
		time.Sleep(time.Until(due))
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
		seq := i
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
				err = w.request(ctx, r, a, "cards", "GET", "/sets/"+setID(1)+"/cards", nil, nil)
			case 4:
				err = w.request(ctx, r, a, "set", "GET", "/sets/"+setID(1), nil, nil)
			case 5, 6:
				err = w.check(ctx, r, a, seq)
			case 7, 8:
				err = w.submit(ctx, r, a, seq, 5)
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
