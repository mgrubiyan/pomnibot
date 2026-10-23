package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

type replayTransport func(*http.Request) (*http.Response, error)

func (f replayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Replay the original lecture SSE through the real transport decoder, with
// an in-memory HTTP transport. This test cannot send a network request.
func TestLectureFlipOnlyRecordingIsRejectedWithoutRepair(t *testing.T) {
	dir := os.Getenv("POMNIBOT_LECTURE_RECORDING")
	if dir == "" {
		t.Skip("private lecture recording required; offline replay only")
	}
	body, err := os.ReadFile(filepath.Join(dir, "response.sse"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Messages []struct{ Role, Content string }
	}
	if err = json.Unmarshal(request, &recorded); err != nil {
		t.Fatal(err)
	}
	var source struct {
		Title  string
		Blocks []material.Block
	}
	for _, msg := range recorded.Messages {
		if msg.Role == "user" {
			if err = json.Unmarshal([]byte(msg.Content), &source); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(source.Blocks) == 0 {
		t.Fatal("missing recorded source")
	}
	calls := 0
	client, err := openrouter.New(openrouter.Config{
		APIKey: "offline", Model: openrouter.DefaultModel, BaseURL: openrouter.DefaultBaseURL, MaxTokens: 50000,
		HTTPClient: &http.Client{Transport: replayTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := providerFunc(func(ctx context.Context, req providers.Request) (providers.Response, error) {
		resp, callErr := client.Complete(ctx, req)
		if callErr != nil {
			return resp, callErr
		}
		// Verify this is the observed 31-fact flip-only response before
		// checking that today's generator refuses to publish it.
		var wire struct {
			Response struct {
				Facts []struct {
					Cards []struct{ Kind string }
				}
			}
		}
		if err := json.Unmarshal(resp.Content, &wire); err != nil {
			t.Fatal(err)
		}
		if len(wire.Response.Facts) != 31 {
			t.Fatalf("expected 31 recorded facts, got %d", len(wire.Response.Facts))
		}
		for _, f := range wire.Response.Facts {
			if len(f.Cards) != 1 || f.Cards[0].Kind != "flip" {
				t.Fatal("expected one flip per recorded fact")
			}
		}
		resp.Usage = providers.Usage{} // Historical billing is not a new expense.
		return resp, nil
	})
	r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published flip-only recording") }}).Generate(context.Background(), Document{Title: source.Title, Structure: &material.Document{Blocks: source.Blocks}})
	if err == nil || calls != 1 || r.Stats.InvalidResponses != 1 || len(r.Facts) != 0 || len(r.Cards) != 0 {
		t.Fatalf("recording accepted or repaired: %+v, %v, calls=%d", r, err, calls)
	}
	t.Log("original SSE: 31 flip-only facts rejected, zero published, one offline request, no repair")
}
