package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHealthEndpoint(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": {Data: []byte("<!DOCTYPE html><html><body>Test</body></html>")},
	}

	router, err := setupRouter(&apiService{}, mockFS)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	expectedBody := `{"status":"ok"}`
	if rec.Body.String() != expectedBody {
		t.Fatalf("expected body %q, got %q", expectedBody, rec.Body.String())
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

	router, err := setupRouter(&apiService{}, mockFS)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
		expectedHeader string
	}{
		{
			name:           "Root path serves index.html",
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
		{
			name:           "Existing asset serves asset",
			path:           "/assets/app.js",
			expectedStatus: http.StatusOK,
			expectedBody:   "console.log('hello');",
			expectedHeader: "public, max-age=31536000, immutable",
		},
		{
			name:           "React router path serves index.html",
			path:           "/feed/card/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "<!DOCTYPE html><html><body>Test App</body></html>",
			expectedHeader: "no-cache, no-store, must-revalidate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("path %s: expected status %d, got %d", tc.path, tc.expectedStatus, rec.Code)
			}

			if rec.Body.String() != tc.expectedBody {
				t.Errorf("path %s: expected body %q, got %q", tc.path, tc.expectedBody, rec.Body.String())
			}

			if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != tc.expectedHeader {
				t.Errorf("path %s: expected Cache-Control %q, got %q", tc.path, tc.expectedHeader, cacheControl)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})

	wrapped := loggingMiddleware(inner)

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
