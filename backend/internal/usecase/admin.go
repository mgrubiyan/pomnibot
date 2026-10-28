package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// Admins see every complaint. They are named by their MAX IDs, which stay
// the same when the database is recreated; the IDs are positive,
// so a user without the chat app never matches.
type Admins struct {
	MaxIDs []int64
	// BrowserTestUser makes the user without a chat account an admin. Only
	// locally: in prod that is whoever opens the site without MAX.
	BrowserTestUser bool
}

// Is reports whether the account belongs to an admin.
func (a Admins) Is(account Account) bool {
	if slices.Contains(a.MaxIDs, account.MaxID) {
		return true
	}
	return a.BrowserTestUser && account.MaxID == 0
}

// AdminService shows admins what readers complained about across all sets.
type AdminService interface {
	IsAdmin(ctx context.Context, userID uuid.UUID) (bool, error)
	// ListCardIssues lists every complaint for an admin, newest first; for
	// anyone else the list is empty.
	ListCardIssues(ctx context.Context, userID uuid.UUID) ([]contracts.AdminCardIssue, error)
}

type adminServiceImpl struct {
	querier  db.Querier
	accounts AccountGetter
	admins   Admins
}

// NewAdminService checks users against admins.
func NewAdminService(querier db.Querier, accounts AccountGetter, admins Admins) AdminService {
	return &adminServiceImpl{querier: querier, accounts: accounts, admins: admins}
}

func (s *adminServiceImpl) IsAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	account, err := s.accounts.GetAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.admins.Is(account), nil
}

func (s *adminServiceImpl) ListCardIssues(ctx context.Context, userID uuid.UUID) ([]contracts.AdminCardIssue, error) {
	admin, err := s.IsAdmin(ctx, userID)
	if err != nil || !admin {
		return []contracts.AdminCardIssue{}, err
	}
	rows, err := s.querier.ListCardIssues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list card issues: %w", err)
	}
	issues := make([]contracts.AdminCardIssue, 0, len(rows))
	for _, r := range rows {
		issue := contracts.AdminCardIssue{
			ID:          uuid.UUID(r.ID.Bytes),
			Reason:      contracts.CardIssueReason(r.Reason),
			Status:      contracts.CardIssueStatus(r.Status),
			Kind:        contracts.CardKind(r.Kind),
			Question:    r.Question,
			Explanation: r.Explanation,
			SourceQuote: r.SourceQuote,
			Reporter:    buildUserContract(r.ReporterID, r.ReporterFirstName, r.ReporterLastName, r.ReporterUsername),
			CreatedAt:   r.CreatedAt.Time,
		}
		if r.SetTitle.Valid {
			issue.SetTitle.SetTo(r.SetTitle.String)
		}
		if r.AnswerText.Valid {
			issue.Answer.SetTo(r.AnswerText.String)
		}
		if r.Comment.Valid {
			issue.Comment.SetTo(r.Comment.String)
		}
		issues = append(issues, issue)
	}
	return issues, nil
}
