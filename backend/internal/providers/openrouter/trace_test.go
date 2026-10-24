package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type traceTransport func(*http.Request) (*http.Response, error)

func (f traceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type staleConnectionTransport struct {
	phase     string
	discarded bool
}

func (t *staleConnectionTransport) CloseIdleConnections() { t.discarded = true }

func (t *staleConnectionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.discarded {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"recovered"}}]}`))}, nil
	}
	if t.phase == "headers" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: traceDeadlineBody{r.Context()}}, nil
}

func TestTimedOutConnectionIsDiscardedBeforeNextCall(t *testing.T) {
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			tr := &staleConnectionTransport{phase: phase}
			client, err := New(Config{APIKey: "test", Model: DefaultModel, BaseURL: DefaultBaseURL, MaxTokens: 8000, HTTPClient: &http.Client{Transport: tr}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			if _, err = client.Complete(ctx, providers.Request{}); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if !tr.discarded {
				t.Fatal("next call would reuse the timed-out connection")
			}
			ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel2()
			response, err := client.Complete(ctx2, providers.Request{})
			if err != nil || string(response.Content) != "recovered" {
				t.Fatalf("retry did not recover: %+v %v", response, err)
			}
		})
	}
}

func TestDefaultClientOwnsItsTransport(t *testing.T) {
	client, err := New(Config{APIKey: "test", Model: DefaultModel, BaseURL: DefaultBaseURL, MaxTokens: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if client.http.Transport == nil || client.http.Transport == http.DefaultTransport {
		t.Fatal("connection recovery would close unrelated clients' idle connections")
	}
}

func TestRecoveryReplacesPoolWhileOldRequestIsActive(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	addresses := make(chan string, 2)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addresses <- r.RemoteAddr
		if r.URL.Path == "/held" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-release
			_, _ = io.WriteString(w, "held response")
			return
		}
		_, _ = io.WriteString(w, "retry response")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	client := server.Client()
	client.Timeout = 3 * time.Second
	client.Transport = newRecoveryTransport(client.Transport.(*http.Transport))
	t.Cleanup(client.CloseIdleConnections)
	held, err := client.Get(server.URL + "/held")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Body.Close() }()
	if held.ProtoMajor != 2 {
		t.Fatal("test requires an active HTTP/2 stream")
	}
	firstAddress := <-addresses
	client.CloseIdleConnections()
	retry, err := client.Get(server.URL + "/retry")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = retry.Body.Close() }()
	if <-addresses == firstAddress {
		t.Fatal("retry reused the connection with an active stream")
	}
	data, err := io.ReadAll(retry.Body)
	if err != nil || string(data) != "retry response" {
		t.Fatalf("retry: %q %v", data, err)
	}
	once.Do(func() { close(release) })
	data, err = io.ReadAll(held.Body)
	if err != nil || string(data) != "held response" {
		t.Fatalf("recovery interrupted another request: %q %v", data, err)
	}
}

type traceDeadlineBody struct{ ctx context.Context }

func (b traceDeadlineBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (traceDeadlineBody) Close() error               { return nil }

func TestCompleteLogsTimeoutPhaseWithoutPrivateData(t *testing.T) {
	for _, phase := range []string{"waiting_response_headers", "reading_response_body"} {
		t.Run(phase, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			transport := traceTransport(func(r *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(r.Context())
				if trace != nil && trace.WroteRequest != nil {
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				if phase == "waiting_response_headers" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				if trace != nil && trace.GotFirstResponseByte != nil {
					trace.GotFirstResponseByte()
				}
				return &http.Response{StatusCode: 200, Body: traceDeadlineBody{r.Context()}, Header: make(http.Header)}, nil
			})
			client, err := New(Config{APIKey: "private-api-key", Model: DefaultModel, BaseURL: DefaultBaseURL, MaxTokens: 8000, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			_, err = client.Complete(ctx, providers.Request{System: "private-system", User: "private-document"})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			s := logs.String()
			if !strings.Contains(s, `"phase":"`+phase+`"`) {
				t.Fatalf("missing diagnostic %s: %s", phase, s)
			}
			for _, secret := range []string{"private-api-key", "private-system", "private-document"} {
				if strings.Contains(s, secret) {
					t.Fatal("private request data logged")
				}
			}
		})
	}
}

// Compile-time check: the simulated response body has the same lifecycle as a
// real body that receives headers but stalls before a complete JSON response.
var _ io.ReadCloser = traceDeadlineBody{}

func TestTimeoutDiagnosticsPreserveResponseProgress(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		complete   bool
	}{
		{"partial", ` {"id":"gen-partial","choices":[`, false},
		{"complete without EOF", ` {"id":"gen-complete","choices":[{"message":{"content":"private-document"}}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			tr := traceTransport(func(r *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(r.Context())
				trace.GotConn(httptrace.GotConnInfo{Reused: true})
				trace.WroteRequest(httptrace.WroteRequestInfo{})
				trace.GotFirstResponseByte()
				return &http.Response{StatusCode: 200, Proto: "HTTP/2.0", Header: http.Header{"X-Request-Id": {"request-test"}, "X-Generation-Id": {"gen-test"}}, Body: struct {
					io.Reader
					io.Closer
				}{io.MultiReader(strings.NewReader(tc.body), traceDeadlineBody{r.Context()}), io.NopCloser(strings.NewReader(""))}}, nil
			})
			client, err := New(Config{APIKey: "private-key", Model: DefaultModel, BaseURL: DefaultBaseURL, MaxTokens: 8000, HTTPClient: &http.Client{Transport: tr}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			_, err = client.Complete(ctx, providers.Request{User: "private-request"})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			var event map[string]any
			found := false
			dec := json.NewDecoder(&logs)
			for dec.Decode(&event) == nil {
				if event["response_bytes"] == float64(len(tc.body)) {
					found = true
					if event["response_json_complete"] != tc.complete || event["connection_reused"] != true || event["protocol"] != "HTTP/2.0" || event["request_id"] != "request-test" || event["generation_id"] != "gen-test" || event["status"] != float64(200) {
						t.Fatalf("incomplete diagnostic: %+v", event)
					}
				}
			}
			if !found {
				t.Fatal("timeout discarded response progress")
			}
			for _, secret := range []string{"private-key", "private-document", "private-request"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("private data logged")
				}
			}
		})
	}
}
