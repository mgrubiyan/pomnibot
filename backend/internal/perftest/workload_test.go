package perftest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnswerJourney(t *testing.T) {
	for _, kind := range []contracts.CardKind{contracts.CardKindFlip, contracts.CardKindBoolean, contracts.CardKindChoice, contracts.CardKindTable, contracts.CardKindInput} {
		t.Run(string(kind), func(t *testing.T) {
			id := uuid.New()
			var paths []string
			var submission contracts.AnswerQuestionRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/api/feed":
					assert.Equal(t, setID(1), r.URL.Query().Get("setId"))
					assert.Equal(t, "true", r.URL.Query().Get("early"))
					_ = json.NewEncoder(w).Encode([]contracts.FeedCard{{ID: id, Kind: kind}})
				case "/api/cards/" + id.String() + "/reveal":
					_, _ = w.Write([]byte(`{}`))
				case "/api/cards/" + id.String() + "/answer":
					if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					_, _ = w.Write([]byte(`{"isCorrect":true,"method":"local"}`))
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			w := workload{server: server, client: server.Client()}
			a := identity(0)
			recorder := newRecorder()
			require.NoError(t, w.answer(t.Context(), recorder, a))
			assert.NotEqual(t, uuid.Nil, submission.AttemptId)
			expected, err := fixtureAnswer(kind)
			require.NoError(t, err)
			assert.Equal(t, expected, submission.Answer)
			assert.EqualValues(t, 1, a.submitted.Load())
			expectedPaths := []string{"GET /api/feed"}
			if kind == contracts.CardKindFlip {
				expectedPaths = append(expectedPaths, "POST /api/cards/"+id.String()+"/reveal")
			}
			expectedPaths = append(expectedPaths, "POST /api/cards/"+id.String()+"/answer")
			assert.Equal(t, expectedPaths, paths)
			assert.True(t, recorder.summary(time.Second).passes())
		})
	}
}

func TestWaitUntilCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.False(t, waitUntil(ctx, time.Now().Add(time.Hour)))
}

func TestAnswerFailureDoesNotCountAsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http error", 500, `{}`},
		{"model fallback", 200, `{"isCorrect":false,"method":"fallback"}`},
		{"wrong verdict", 200, `{"isCorrect":false,"method":"local"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/feed" {
					_ = json.NewEncoder(w).Encode([]contracts.FeedCard{{ID: id, Kind: contracts.CardKindInput}})
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			w := workload{server: server, client: server.Client()}
			a := identity(0)
			recorder := newRecorder()
			require.Error(t, w.answer(t.Context(), recorder, a))
			assert.Zero(t, a.submitted.Load())
			assert.False(t, recorder.summary(time.Second).passes())
		})
	}
}
