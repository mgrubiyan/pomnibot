package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
)

func TestGetFileSystem(t *testing.T) {
	fsys, err := getFileSystem()
	if err != nil {
		t.Fatalf("unexpected error getting embedded filesystem: %v", err)
	}
	if fsys == nil {
		t.Fatal("expected non-nil filesystem")
	}
}

func TestServerIntegration(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": {Data: []byte("<!DOCTYPE html><html><body>Integration Test</body></html>")},
	}

	router, err := httptransport.NewRouter(httptransport.NewAPIHandler(), mockFS)
	if err != nil {
		t.Fatalf("failed to setup router: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
