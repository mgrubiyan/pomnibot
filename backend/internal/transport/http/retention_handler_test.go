package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type retentionSetService struct {
	usecase.SetService
	result    *contracts.SetRetention
	err       error
	user, set uuid.UUID
}

func (s *retentionSetService) GetSetRetention(_ context.Context, user, set uuid.UUID) (*contracts.SetRetention, error) {
	s.user, s.set = user, set
	return s.result, s.err
}

func TestRetentionHandler(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"}, {name: "not found", err: usecase.ErrNotFound}, {name: "forbidden hidden", err: usecase.ErrForbidden}, {name: "server error", err: errors.New("private database details")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, set := uuid.New(), uuid.New()
			service := &retentionSetService{err: tc.err, result: &contracts.SetRetention{CalculatedAt: time.Now().UTC(), HorizonDays: 13, Target: 0.9, TotalFacts: 1, Current: 0.75, Groups: contracts.RetentionGroups{From70To90: 1}, Points: []contracts.RetentionPoint{{Day: 0, WithReviews: 0.75, WithoutReviews: 0.75}, {Day: 13, WithReviews: 0.95, WithoutReviews: 0.5}}, WithReviews: 0.95, WithoutReviews: 0.5}}
			h := &APIHandler{sets: NewSetsHandler(service)}
			res, err := h.GetSetRetention(WithUserID(t.Context(), user), contracts.GetSetRetentionParams{SetId: set})
			if err != nil || service.user != user || service.set != set {
				t.Fatalf("delegation: %v", err)
			}
			switch tc.err {
			case nil:
				value, ok := res.(*contracts.SetRetention)
				if !ok || value != service.result {
					t.Fatalf("response: %T", res)
				}
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var decoded map[string]any
				if err = json.Unmarshal(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded["current"] != 0.75 || decoded["horizonDays"] != float64(13) {
					t.Fatalf("JSON: %s", encoded)
				}
			case usecase.ErrNotFound, usecase.ErrForbidden:
				if _, ok := res.(*contracts.GetSetRetentionNotFound); !ok {
					t.Fatalf("response: %T", res)
				}
			default:
				value, ok := res.(*contracts.GetSetRetentionInternalServerError)
				if !ok || value.Message == "" {
					t.Fatalf("missing error message: %+v", res)
				}
			}
		})
	}
}

func TestRetentionHTTPContract(t *testing.T) {
	user, set := uuid.New(), uuid.New()
	service := &retentionSetService{result: &contracts.SetRetention{
		CalculatedAt: time.Now().UTC(), HorizonDays: 13, Target: 0.9, TotalFacts: 0,
		Points: []contracts.RetentionPoint{{Day: 0}, {Day: 13}},
	}}
	handler := NewAPIHandler(service, nil, nil, nil)
	// Exercise generated routing and JSON encoding with the authenticated user.
	server, err := contracts.NewServer(handler, contracts.WithPathPrefix("/api"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/sets/"+set.String()+"/retention", nil)
	req = req.WithContext(WithUserID(req.Context(), user))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || service.user != user || service.set != set {
		t.Fatalf("HTTP %d: %s", recorder.Code, recorder.Body.String())
	}
	var result contracts.SetRetention
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.HorizonDays != 13 || len(result.Points) != 2 {
		t.Fatalf("JSON %s: %v", recorder.Body.String(), err)
	}
	service.err = usecase.ErrNotFound
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("HTTP %d", recorder.Code)
	}
	// The endpoint remains behind the same authentication middleware as all set APIs.
	router, err := NewRouter(handler, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("app")}}, "token", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	invalid := httptest.NewRequest(http.MethodGet, req.URL.Path, nil)
	invalid.Header.Set("Authorization", "max invalid")
	router.ServeHTTP(recorder, invalid)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credentials HTTP %d", recorder.Code)
	}
}
