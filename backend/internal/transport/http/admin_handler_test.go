package http

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

type adminServiceStub struct {
	admin  bool
	err    error
	issues []contracts.AdminCardIssue
	userID uuid.UUID
}

func (s *adminServiceStub) IsAdmin(_ context.Context, userID uuid.UUID) (bool, error) {
	s.userID = userID
	return s.admin, s.err
}

func (s *adminServiceStub) ListCardIssues(_ context.Context, userID uuid.UUID) ([]contracts.AdminCardIssue, error) {
	s.userID = userID
	return s.issues, s.err
}

func TestAPIHandler_GetAdminIssues(t *testing.T) {
	testUserID := uuid.New()
	ctx := WithUserID(context.Background(), testUserID)
	issue := contracts.AdminCardIssue{ID: uuid.New(), Question: "Q"}

	admin := &adminServiceStub{issues: []contracts.AdminCardIssue{issue}}
	res, err := NewAPIHandler(nil, nil, nil, nil).WithAdmin(admin).GetAdminIssues(ctx)
	if got, ok := res.(*contracts.GetAdminIssuesOKApplicationJSON); err != nil || !ok || len(*got) != 1 || (*got)[0].ID != issue.ID || admin.userID != testUserID {
		t.Fatalf("admin: %#v, %v", res, err)
	}

	res, err = NewAPIHandler(nil, nil, nil, nil).WithAdmin(&adminServiceStub{}).GetAdminIssues(ctx)
	if got, ok := res.(*contracts.GetAdminIssuesOKApplicationJSON); err != nil || !ok || got == nil || *got == nil {
		t.Fatalf("not an admin must get an empty list, not null: %#v, %v", res, err)
	}

	res, err = NewAPIHandler(nil, nil, nil, nil).WithAdmin(&adminServiceStub{err: errors.New("db down")}).GetAdminIssues(ctx)
	if _, ok := res.(*contracts.GetAdminIssuesInternalServerError); err != nil || !ok {
		t.Fatalf("failure: %#v, %v", res, err)
	}
}

func TestAPIHandler_GetTodayMarksAdmin(t *testing.T) {
	testUserID := uuid.New()
	ctx := WithUserID(context.Background(), testUserID)
	home := &mockHomescreenService{getTodayFunc: func(context.Context, uuid.UUID) (*contracts.TodayData, error) {
		return &contracts.TodayData{User: contracts.User{ID: testUserID}}, nil
	}}
	tests := []struct {
		name  string
		admin *adminServiceStub
		want  bool
	}{
		{"admin", &adminServiceStub{admin: true}, true},
		{"not an admin", &adminServiceStub{}, false},
		// Home opens without the admin row rather than not at all.
		{"check failed", &adminServiceStub{admin: true, err: errors.New("db down")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := NewAPIHandler(nil, nil, home, nil).WithAdmin(tt.admin).GetToday(ctx)
			today, ok := res.(*contracts.TodayData)
			if err != nil || !ok {
				t.Fatalf("GetToday = %#v, %v", res, err)
			}
			if got := today.IsAdmin.Value; got != tt.want || tt.admin.userID != testUserID {
				t.Fatalf("isAdmin = %v for user %s, want %v", got, tt.admin.userID, tt.want)
			}
		})
	}
}
