package openrouter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type traceTransport func(*http.Request) (*http.Response, error)

func (f traceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
