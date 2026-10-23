package generator

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

type replayTransport func(*http.Request) (*http.Response, error)

func (f replayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// These complete SSE bodies were recorded on 2026-10-22 from OpenRouter
// (anthropic/claude-haiku-5.5, Azure endpoint, reasoning high). The .sse.gz files
// retain the original bytes, compressed only for storage; the .json files were
// assembled independently at the transport boundary. Source blocks are in
// source.json. No credentials or authorization headers are included.
// Each response finished with stop but contained unusable cards. Compare the
// transport output with the recorded JSON before checking rejection without
// repair; these fixtures do not measure model reliability or coverage.
func TestRecordedStubsSurviveTransportAndAreRejectedWithoutRepair(t *testing.T) {
	for _, attempt := range []int{4, 6, 8} {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			path := fmt.Sprintf("testdata/recorded-stubs/%d", attempt)
			compressed, err := os.ReadFile(path + ".sse.gz")
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(path + ".json")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client, err := openrouter.New(openrouter.Config{
				APIKey: "offline", Model: openrouter.DefaultModel, BaseURL: openrouter.DefaultBaseURL, MaxTokens: 50000,
				HTTPClient: &http.Client{Transport: replayTransport(func(*http.Request) (*http.Response, error) {
					calls++
					body, readErr := gzip.NewReader(bytes.NewReader(compressed))
					if readErr != nil {
						return nil, readErr
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			p := providerFunc(func(ctx context.Context, req providers.Request) (providers.Response, error) {
				response, callErr := client.Complete(ctx, req)
				if callErr != nil {
					return response, callErr
				}
				var got, want any
				if json.Unmarshal(response.Content, &got) != nil || json.Unmarshal(expected, &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("transport changed recorded content")
				}
				if response.Truncated || response.Usage.CostUSD <= 0 {
					t.Fatal("recording must finish normally with reported usage")
				}
				return response, nil
			})
			source, err := os.ReadFile("testdata/recorded-stubs/source.json")
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Title  string
				Blocks []material.Block
			}
			if err = json.Unmarshal(source, &doc); err != nil {
				t.Fatal(err)
			}
			r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published stub") }}).Generate(context.Background(), Document{Title: doc.Title, Structure: &material.Document{Blocks: doc.Blocks}})
			if err == nil || calls != 1 || len(r.Cards) != 0 || len(r.Facts) != 0 || r.Stats.InvalidResponses != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", r, err, calls)
			}
		})
	}
}

// Isolate the two defects in the observed stub: neither assertion may pass
// merely because the other defect caused decoding to stop first.
func TestStubDefectsAreValidatedIndependently(t *testing.T) {
	var response AIResponse
	if err := json.Unmarshal([]byte(`{"facts":[{"name":"F","cards":[{"kind":"choice","question":"Q","rightAnswers":["A"],"wrongAnswers":["B"]},{"kind":"input","question":"Recall","answer":"A"}]}]}`), &response); err == nil {
		t.Fatal("accepted fact without flip")
	}
	if err := json.Unmarshal([]byte(`{"facts":[{"name":"F","cards":[{"kind":"flip","question":"Q","answer":"A"},{"kind":"choice","question":"Choose","rightAnswers":["A"],"wrongAnswers":[" a "]},{"kind":"input","question":"Recall","answer":"A"}]}]}`), &response); err != nil {
		t.Fatal(err)
	}
	if _, valid := storedCard(response.Facts[0].Cards[1]); valid {
		t.Fatal("accepted overlapping choice answers despite valid flip")
	}
}
