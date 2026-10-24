package http

import (
	"context"
	"log/slog"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// WithAdmin lets admins see every complaint.
func (h *APIHandler) WithAdmin(admin usecase.AdminService) *APIHandler {
	h.admin = admin
	return h
}

// GetAdminIssues implements contracts.Handler.GetAdminIssues.
func (h *APIHandler) GetAdminIssues(ctx context.Context) (contracts.GetAdminIssuesRes, error) {
	if h.admin == nil {
		return h.UnimplementedHandler.GetAdminIssues(ctx)
	}
	userID, _ := UserIDFromContext(ctx)
	issues, err := h.admin.ListCardIssues(ctx, userID)
	if err != nil {
		return &contracts.GetAdminIssuesInternalServerError{Message: internalError(ctx, "GetAdminIssues", err)}, nil
	}
	res := contracts.GetAdminIssuesOKApplicationJSON(issues)
	if res == nil {
		res = contracts.GetAdminIssuesOKApplicationJSON{}
	}
	return &res, nil
}

// markAdmin shows an admin the way to every complaint. A failed check leaves
// the mark off: the home screen opens without it.
func (h *APIHandler) markAdmin(ctx context.Context, today *contracts.TodayData) {
	if h.admin == nil {
		return
	}
	userID, _ := UserIDFromContext(ctx)
	admin, err := h.admin.IsAdmin(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "admin check failed", "user_id", userID, "error", err)
		return
	}
	if admin {
		today.IsAdmin.SetTo(true)
	}
}
