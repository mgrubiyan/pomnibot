package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

func TestHealthEndpoint(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": {Data: []byte("<!DOCTYPE html><html><body>Test</body></html>")},
	}

	router, err := NewRouter(NewAPIHandler(), mockFS)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response contracts.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if response.Status != "ok" {
		t.Fatalf("expected status %q, got %q", "ok", response.Status)
	}
}

func TestStaticAndReactRouter(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": {
			Data: []byte("<!DOCTYPE html><html><body>Test App</body></html>"),
		},
		"assets/app.js": {
			Data: []byte("console.log('hello');"),
		},
	}

	router, err := NewRouter(NewAPIHandler(), mockFS)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
		expectedHeader string
		expectedAllow  string
	}{
		{
			name:           "Root path serves index.html",
			method:         http.MethodGet,
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "Existing asset serves asset",
			method:         http.MethodGet,
			path:           "/assets/app.js",
			expectedStatus: http.StatusOK,
			expectedBody:   "console.log('hello');",
			expectedHeader: "public, max-age=31536000, immutable",
		},
		{
			name:           "React router path serves index.html",
			method:         http.MethodGet,
			path:           "/feed/card/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "React router HEAD serves index headers",
			method:         http.MethodHead,
			path:           "/feed/card/123",
			expectedStatus: http.StatusOK,
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "Unknown API path returns 404 for GET",
			method:         http.MethodGet,
			path:           "/api/cards",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Unknown API path returns 404 for POST",
			method:         http.MethodPost,
			path:           "/api/cards",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Missing asset returns 404",
			method:         http.MethodGet,
			path:           "/assets/missing.js",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Non-GET/HEAD request on missing route returns 405",
			method:         http.MethodPost,
			path:           "/feed/card/123",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedAllow:  "GET, HEAD",
		},
		{
			name:           "DELETE request on unknown path returns 405",
			method:         http.MethodDelete,
			path:           "/whatever",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedAllow:  "GET, HEAD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("path %s: expected status %d, got %d", tc.path, tc.expectedStatus, rec.Code)
			}

			if tc.expectedBody != "" && rec.Body.String() != tc.expectedBody {
				t.Errorf("path %s: expected body %q, got %q", tc.path, tc.expectedBody, rec.Body.String())
			}

			if tc.expectedHeader != "" {
				if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != tc.expectedHeader {
					t.Errorf("path %s: expected Cache-Control %q, got %q", tc.path, tc.expectedHeader, cacheControl)
				}
			}

			if tc.expectedAllow != "" {
				if allow := rec.Header().Get("Allow"); allow != tc.expectedAllow {
					t.Errorf("path %s: expected Allow header %q, got %q", tc.path, tc.expectedAllow, allow)
				}
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})

	wrapped := LoggingMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/custom-path", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rec.Code)
	}

	if rec.Body.String() != "short and stout" {
		t.Fatalf("expected body %q, got %q", "short and stout", rec.Body.String())
	}
}

func BenchmarkSPAHandler_Asset(b *testing.B) {
	mockFS := fstest.MapFS{
		"index.html":    {Data: []byte("<!DOCTYPE html><html><body>Test App</body></html>")},
		"assets/app.js": {Data: []byte("console.log('hello');")},
	}
	handler := NewSPAHandler(mockFS)
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
