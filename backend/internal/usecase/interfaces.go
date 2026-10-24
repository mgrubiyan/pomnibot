package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
)

// SetService defines domain operations on card sets.
type SetService interface {
	CreateSet(context.Context, uuid.UUID, *contracts.CreateSetRequest) (*contracts.CardSet, error)
	GetSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error)
	DeleteSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error
	GetCardsBySetID(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.Card, error)
	GetSetShareCode(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error)
	GetSetPlan(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetPlanItem, error)
	GetSetLeaderboard(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetLeaderboardResponse, error)
	JoinSetByShareCode(ctx context.Context, userID uuid.UUID, code string) (*contracts.CardSet, error)
	SaveGeneratedSet(ctx context.Context, userID uuid.UUID, title string, genResult generator.Result, warnings []string) (*contracts.CardSet, error)
}

// CardService defines domain operations on individual cards.
type CardService interface {
	CreateCard(context.Context, uuid.UUID, uuid.UUID, *contracts.CreateCardRequest) (*contracts.Card, error)
	UpdateCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	DeleteCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
	AnswerQuestion(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error)
	RevealCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (*contracts.CardReveal, error)
	ReportCardIssue(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, reason contracts.CardIssueReason) error
	GetSetIssues(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetIssue, error)
	ResolveCardIssues(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
}

// HomescreenService defines domain operations for today overview and the feed.
type HomescreenService interface {
	GetToday(ctx context.Context, userID uuid.UUID) (*contracts.TodayData, error)
	GetFeedQuestions(ctx context.Context, userID uuid.UUID) ([]contracts.FeedCard, error)
}

// MAXUserParams comes from a verified MAX request or bot update.
type MAXUserParams struct {
	MaxID              int64
	FirstName          string
	LastName, Username *string
	IsBot              bool
}

// TelegramUserParams never overwrites the manually entered profile names.
type TelegramUserParams struct {
	TgID              int64
	Username          string
	IsBot, BotStarted bool
}

// Account contains the saved user and Telegram conversation state.
// User.ID is internal; MaxID and TgID are only for messenger delivery.
type Account struct {
	User            contracts.User
	MaxID, TgID     int64
	ProfileComplete bool
	ProfileStep     string
	BotStarted      bool
}

// UserService defines user domain operations.
type UserService interface {
	UpsertMAXUser(context.Context, MAXUserParams) (Account, error)
	UpsertTelegramUser(context.Context, TelegramUserParams) (Account, error)
	GetAccount(context.Context, uuid.UUID) (Account, error)
	SaveProfile(context.Context, uuid.UUID, string, string) (Account, error)
	ProfileInput(context.Context, uuid.UUID, string, bool) (Account, error)
	EnsureUser(ctx context.Context, userID uuid.UUID) error
	GetUsersWithDueFacts(ctx context.Context, now time.Time, tz string) ([]uuid.UUID, error)
}
