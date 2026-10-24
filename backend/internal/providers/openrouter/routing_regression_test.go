package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// The paid comparison found unusable completions on automatic routing. The
// selected endpoint must not silently fall back to that path on a later call.
func TestEnvironmentRoutingPinsDefaultHaikuAndAllowsExplicitOverrides(t *testing.T) {
	for _, tc := range []struct{ name, model, endpoint, want string }{
		{name: "verified default", want: "Anthropic"},
		{name: "explicit endpoint", endpoint: "Google", want: "Google"},
		{name: "explicit automatic routing", endpoint: "auto"},
		{name: "other model keeps automatic routing", model: "other/model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS", "OPENROUTER_EFFORT", "OPENROUTER_PROVIDER"} {
				t.Setenv(key, "")
			}
			t.Setenv("OPENROUTER_KEY", "offline")
			t.Setenv("OPENROUTER_MODEL", tc.model)
			t.Setenv("OPENROUTER_PROVIDER", tc.endpoint)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var wire struct {
					Provider map[string]json.RawMessage `json:"provider"`
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				var only []string
				if raw, ok := wire.Provider["only"]; ok {
					if err := json.Unmarshal(raw, &only); err != nil {
						t.Error(err)
					}
				}
				if tc.want == "" {
					if len(only) != 0 {
						t.Errorf("unexpected endpoint pin: %v", only)
					}
					if _, ok := wire.Provider["allow_fallbacks"]; ok {
						t.Error("automatic routing unexpectedly disables fallback")
					}
				} else {
					if len(only) != 1 || only[0] != tc.want {
						t.Errorf("routing=%v want only %s", only, tc.want)
					}
					if string(wire.Provider["allow_fallbacks"]) != "false" {
						t.Error("endpoint pin must forbid fallback")
					}
				}
				_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			t.Setenv("OPENROUTER_BASE_URL", server.URL)
			cfg, err := ConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			cfg.HTTPClient = server.Client()
			client, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Complete(context.Background(), providers.Request{}); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
