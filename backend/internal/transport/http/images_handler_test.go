package http

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type imageServiceStub struct {
	user, asset, set uuid.UUID
	data             []byte
}

func (s imageServiceStub) GetSetImages(_ context.Context, user, set uuid.UUID) ([]contracts.CardImage, error) {
	if user != s.user || set != s.set {
		return nil, usecase.ErrNotFound
	}
	return []contracts.CardImage{}, nil
}
func (s imageServiceStub) GetCardImage(_ context.Context, user, asset uuid.UUID) ([]byte, error) {
	if user != s.user || asset != s.asset {
		return nil, usecase.ErrNotFound
	}
	return s.data, nil
}
func TestImageEndpointsUseCurrentUserAndReturnBinary(t *testing.T) {
	owner, asset, set := uuid.New(), uuid.New(), uuid.New()
	data := []byte("\x89PNG\r\n\x1a\nimage")
	server, err := contracts.NewServer(NewAPIHandler(nil, nil, nil, nil).WithImages(imageServiceStub{owner, asset, set, data}), contracts.WithPathPrefix("/api"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		user uuid.UUID
		code int
		body []byte
	}{{"/api/images/" + asset.String(), owner, 200, data}, {"/api/images/" + asset.String(), uuid.New(), 404, nil}, {"/api/sets/" + set.String() + "/images", owner, 200, []byte("[]")}, {"/api/sets/" + set.String() + "/images", uuid.New(), 404, nil}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r = r.WithContext(WithUserID(r.Context(), tc.user))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != tc.code || tc.body != nil && !bytes.Equal(bytes.TrimSpace(w.Body.Bytes()), tc.body) {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if bytes.Contains(tc.body, []byte("PNG")) && (w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-store") {
			t.Fatal("wrong image headers", w.Header())
		}
	}
}
