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
	GetSetRetention(context.Context, uuid.UUID, uuid.UUID) (*contracts.SetRetention, error)
	CreateSet(context.Context, uuid.UUID, *contracts.CreateSetRequest) (*contracts.CardSet, error)
	GetSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error)
	UpdateSetTitle(ctx context.Context, userID uuid.UUID, setID uuid.UUID, title string) error
	DeleteSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error
	GetCardsBySetID(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.Card, error)
	GetSetShareCode(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error)
	GetSetPlan(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetPlanItem, error)
	GetSetLeaderboard(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetLeaderboardResponse, error)
	GetSetMistakes(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetMistakesResponse, error)
	JoinSetByShareCode(ctx context.Context, userID uuid.UUID, code string) (*contracts.CardSet, error)
	SaveGeneratedSet(ctx context.Context, userID uuid.UUID, title string, genResult generator.Result, warnings []string) (*contracts.CardSet, error)
}

// FactService defines domain operations on facts within card sets.
type FactService interface {
	GetFactsBySetID(ctx context.Context, userID, setID uuid.UUID) ([]contracts.Fact, error)
	CreateFact(ctx context.Context, userID, setID uuid.UUID, name string) (*contracts.Fact, error)
	UpdateFact(ctx context.Context, userID, factID uuid.UUID, name string) error
	DeleteFact(ctx context.Context, userID, factID uuid.UUID) error
	ReorderCards(ctx context.Context, userID, factID uuid.UUID, cardIDs []uuid.UUID) error
}

// CardService defines domain operations on individual cards.
type CardService interface {
	CreateCard(context.Context, uuid.UUID, uuid.UUID, *contracts.CreateCardRequest) (*contracts.Card, error)
	CreateCardInFact(ctx context.Context, userID, factID uuid.UUID, req *contracts.CreateCardRequest) (*contracts.Card, error)
	UpdateCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, req *contracts.UpdateCardRequest) (*contracts.Card, error)
	DeleteCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
	AnswerQuestion(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, answer string, attemptID uuid.UUID) (*contracts.AnswerQuestionResponse, error)
	RevealCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (*contracts.CardReveal, error)
	ReportCardIssue(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, reason contracts.CardIssueReason, comment string) error
	GetSetIssues(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetIssue, error)
	ResolveCardIssues(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) error
}

// FeedOptions controls which cards are selected for the feed.
type FeedOptions struct {
	SetID *uuid.UUID
	Early bool
}

// HomescreenService defines domain operations for today overview and the feed.
type HomescreenService interface {
	GetToday(ctx context.Context, userID uuid.UUID) (*contracts.TodayData, error)
	GetFeedQuestions(ctx context.Context, userID uuid.UUID, opts FeedOptions) ([]contracts.FeedCard, error)
}

// MAXUserParams comes from a verified MAX request or bot update.
type MAXUserParams struct {
	MaxID              int64
	FirstName          string
	LastName, Username *string
	IsBot              bool
}

// Account contains the saved user.
// User.ID is internal; MaxID is only for messenger delivery.
type Account struct {
	User  contracts.User
	MaxID int64
	// DataConsent lets set authors see the user's name.
	DataConsent bool
	// DataConsentAnswered: the user has said yes or no to it once, so the
	// mini-app no longer asks.
	DataConsentAnswered bool
	// AvatarSource is the messenger photo address the avatar was taken
	// from; empty for an uploaded photo and for none.
	AvatarSource string
}

// AvatarImporter keeps the user's photo in step with their messenger profile.
type AvatarImporter interface {
	ImportAvatar(ctx context.Context, id uuid.UUID, source string, data []byte) (Account, error)
	ForgetImportedAvatar(ctx context.Context, id uuid.UUID) error
}

// UserService defines user domain operations.
type UserService interface {
	UpsertMAXUser(context.Context, MAXUserParams) (Account, error)
	GetAccount(context.Context, uuid.UUID) (Account, error)
	SetDataConsent(context.Context, uuid.UUID, bool) (Account, error)
	SaveAvatar(context.Context, uuid.UUID, []byte) (Account, error)
	DeleteAvatar(context.Context, uuid.UUID) (Account, error)
	AvatarImage(context.Context, uuid.UUID) ([]byte, error)
	EnsureUser(ctx context.Context, userID uuid.UUID) error
	GetUsersWithDueFacts(ctx context.Context, now time.Time, tz string) ([]uuid.UUID, error)
}
