package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type userServiceImpl struct {
	querier db.Querier
}

// NewUserService creates a new UserService implementation backed by db.Querier.
func NewUserService(querier db.Querier) UserService {
	return &userServiceImpl{querier: querier}
}

func (s *userServiceImpl) UpsertMAXUser(ctx context.Context, params MAXUserParams) (Account, error) {
	if params.MaxID <= 0 {
		return Account{}, ErrValidation
	}
	row, err := s.querier.UpsertMAXUser(ctx, db.UpsertMAXUserParams{
		MaxID: pgtype.Int8{Int64: params.MaxID, Valid: true}, FirstName: params.FirstName,
		LastName: optionalText(params.LastName), Username: optionalText(params.Username), IsBot: params.IsBot,
	})
	if err != nil {
		return Account{}, fmt.Errorf("upsert MAX user: %w", err)
	}
	return accountFromUser(row), nil
}

func optionalText(value *string) pgtype.Text {
	if value == nil || *value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func (s *userServiceImpl) UpsertTelegramUser(ctx context.Context, params TelegramUserParams) (Account, error) {
	if params.TgID <= 0 {
		return Account{}, ErrValidation
	}
	row, err := s.querier.UpsertTelegramUser(ctx, db.UpsertTelegramUserParams{
		TgID: pgtype.Int8{Int64: params.TgID, Valid: true}, Username: params.Username, IsBot: params.IsBot, TgBotStarted: params.BotStarted,
	})
	if err != nil {
		return Account{}, fmt.Errorf("upsert Telegram user: %w", err)
	}
	return accountFromUser(row), nil
}

func (s *userServiceImpl) GetAccount(ctx context.Context, id uuid.UUID) (Account, error) {
	var pgID pgtype.UUID
	_ = pgID.Scan(id.String())
	row, err := s.querier.GetUserByID(ctx, pgID)
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return accountFromUser(row), nil
}

func accountFromUser(row db.User) Account {
	var uid uuid.UUID
	if row.ID.Valid {
		uid = row.ID.Bytes
	}
	user := contracts.User{ID: uid, FirstName: row.FirstName}
	if row.LastName.Valid {
		user.LastName.SetTo(row.LastName.String)
	}
	if row.Username.Valid {
		user.Username.SetTo(row.Username.String)
	}
	setAvatar(&user, row.AvatarID)
	return Account{User: user, MaxID: row.MaxID.Int64, TgID: row.TgID.Int64,
		ProfileComplete: row.ProfileComplete, ProfileStep: row.ProfileStep, BotStarted: row.TgBotStarted,
		DataConsent: row.DataConsentAt.Valid}
}

// EnsureUser ensures a record exists for the internal user ID, including the browser test user.
func (s *userServiceImpl) EnsureUser(ctx context.Context, userID uuid.UUID) error {
	var pgID pgtype.UUID
	_ = pgID.Scan(userID.String())
	_, err := s.querier.EnsureUser(ctx, pgID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to ensure user in database", "userID", userID, "error", err)
		return fmt.Errorf("ensure user %s: %w", userID, err)
	}
	return nil
}

// GetUsersWithDueFacts returns all user IDs who have cards due today or earlier in the given timezone.
func (s *userServiceImpl) GetUsersWithDueFacts(ctx context.Context, now time.Time, tz string) ([]uuid.UUID, error) {
	if strings.TrimSpace(tz) == "" {
		tz = "Europe/Moscow"
	}

	pgNow := pgtype.Timestamptz{
		Time:  now,
		Valid: true,
	}

	rawUsers, err := s.querier.GetUsersWithDueFacts(ctx, db.GetUsersWithDueFactsParams{
		Tz:  tz,
		Now: pgNow,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to get users with due facts from database", "error", err)
		return nil, fmt.Errorf("get users with due facts: %w", err)
	}

	users := make([]uuid.UUID, 0, len(rawUsers))
	for _, id := range rawUsers {
		if !id.Valid || id.Bytes == uuid.Nil {
			continue
		}
		users = append(users, id.Bytes)
	}

	return users, nil
}
