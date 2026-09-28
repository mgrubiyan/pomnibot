package usecase

import (
	"context"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

// SetService defines domain operations on card sets.
type SetService interface {
	GetSet(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	DeleteSet(ctx context.Context, userID int64, setID uuid.UUID) error
	GetCardsBySetID(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error)
	GetSetShareCode(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	GetSetPlan(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error)
	JoinSetByShareCode(ctx context.Context, userID int64, code string) (*contracts.CardSet, error)
}

// CardService defines domain operations on individual cards.
type CardService interface {
	UpdateCard(ctx context.Context, userID int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	DeleteCard(ctx context.Context, userID int64, cardID uuid.UUID) error
	AnswerQuestion(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error)
	ReportCardIssue(ctx context.Context, userID int64, cardID uuid.UUID, reason contracts.CardIssueReason) error
}

// HomescreenService defines domain operations for today overview, feed, and submitting review results.
type HomescreenService interface {
	GetToday(ctx context.Context, userID int64) (*contracts.TodayData, error)
	GetFeedQuestions(ctx context.Context, userID int64) ([]contracts.Card, error)
	SendResults(ctx context.Context, userID int64, results []contracts.AnswerResult) error
}

// UpsertUserParams defines fields required to upsert a user into the system.
type UpsertUserParams struct {
	ID        int64
	FirstName string
	LastName  *string
	Username  *string
	IsBot     bool
}

// UserService defines user domain operations.
type UserService interface {
	UpsertUser(ctx context.Context, params UpsertUserParams) error
	EnsureUser(ctx context.Context, userID int64) error
}
