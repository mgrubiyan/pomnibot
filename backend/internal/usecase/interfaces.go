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
	CreateSet(context.Context, int64, *contracts.CreateSetRequest) (*contracts.CardSet, error)
	GetSet(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	DeleteSet(ctx context.Context, userID int64, setID uuid.UUID) error
	GetCardsBySetID(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error)
	GetSetShareCode(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error)
	GetSetPlan(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error)
	GetSetLeaderboard(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.SetLeaderboardResponse, error)
	JoinSetByShareCode(ctx context.Context, userID int64, code string) (*contracts.CardSet, error)
	SaveGeneratedSet(ctx context.Context, userID int64, title string, genResult generator.Result) (*contracts.CardSet, error)
}

// CardService defines domain operations on individual cards.
type CardService interface {
	CreateCard(context.Context, int64, uuid.UUID, *contracts.CreateCardRequest) (*contracts.Card, error)
	UpdateCard(ctx context.Context, userID int64, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	DeleteCard(ctx context.Context, userID int64, cardID uuid.UUID) error
	AnswerQuestion(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error)
	CheckAnswer(ctx context.Context, userID int64, cardID uuid.UUID, answer string) (*contracts.CheckAnswerResponse, error)
}

// HomescreenService defines domain operations for today overview, feed, and submitting review results.
type HomescreenService interface {
	GetToday(ctx context.Context, userID int64) (*contracts.TodayData, error)
	GetFeedQuestions(ctx context.Context, userID int64) ([]contracts.Card, error)
	SendResults(ctx context.Context, userID int64, results []contracts.AnswerResult) error
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
	GetAccount(context.Context, int64) (Account, error)
	SaveProfile(context.Context, int64, string, string) (Account, error)
	ProfileInput(context.Context, int64, string, bool) (Account, error)
	EnsureUser(ctx context.Context, userID int64) error
	GetUsersWithDueFacts(ctx context.Context, now time.Time, tz string) ([]int64, error)
}
