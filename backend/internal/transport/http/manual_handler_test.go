package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type manualSetService struct {
	usecase.SetService
	err   error
	calls int
}

func (s *manualSetService) CreateSet(_ context.Context, _ int64, req *contracts.CreateSetRequest) (*contracts.CardSet, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &contracts.CardSet{ID: uuid.New(), Title: req.Title}, nil
}

type manualCardService struct {
	usecase.CardService
	err   error
	calls int
}

func (s *manualCardService) CreateCard(_ context.Context, _ int64, setID uuid.UUID, req *contracts.CreateCardRequest) (*contracts.Card, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &contracts.Card{ID: uuid.New(), SetId: setID, Kind: req.Kind, Question: req.Question, Answer: req.Answer}, nil
}
func TestManualCreationHTTP(t *testing.T) {
	setID := uuid.NewString()
	for _, tt := range []struct {
		name, path, body string
		err              error
		want             int
	}{
		{"set created", "/api/sets", `{"title":"Manual","card":{"kind":"boolean","question":"Q","answer":false}}`, nil, 201},
		{"card created", "/api/sets/" + setID + "/cards", `{"kind":"boolean","question":"Q","answer":false}`, nil, 201},
		{"set validation", "/api/sets", `{"title":"Manual","card":{"kind":"flip","question":"Q","answer":"A"}}`, usecase.ErrValidation, 400},
		{"set persistence failure", "/api/sets", `{"title":"Manual","card":{"kind":"flip","question":"Q","answer":"A"}}`, errors.New("db failure"), 500},
		{"card validation", "/api/sets/" + setID + "/cards", `{"kind":"flip","question":"Q","answer":"A"}`, usecase.ErrValidation, 400},
		{"card not found", "/api/sets/" + setID + "/cards", `{"kind":"flip","question":"Q","answer":"A"}`, usecase.ErrNotFound, 404},
		{"card forbidden hidden", "/api/sets/" + setID + "/cards", `{"kind":"flip","question":"Q","answer":"A"}`, usecase.ErrForbidden, 404},
		{"card persistence failure", "/api/sets/" + setID + "/cards", `{"kind":"flip","question":"Q","answer":"A"}`, errors.New("db failure"), 500},
		{"missing answer", "/api/sets/" + setID + "/cards", `{"kind":"boolean","question":"Q"}`, nil, 400},
		{"missing first card", "/api/sets", `{"title":"Manual"}`, nil, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sets := &manualSetService{err: tt.err}
			cards := &manualCardService{err: tt.err}
			router, err := NewRouter(NewAPIHandler(sets, cards, nil), fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("test")}}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want == 201 && sets.calls+cards.calls != 1 {
				t.Fatal("creation not delegated")
			}
		})
	}
}
