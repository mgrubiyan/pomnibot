package usecase

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// Identity is an external messenger identity verified by a transport.
type Identity struct {
	Platform                      string
	ExternalID                    int64
	FirstName, LastName, Username string
	IsBot, BotStarted             bool
}

// Account contains the saved profile and persistent Telegram conversation.
type Account struct {
	User            contracts.User
	Platform        string
	ExternalID      int64
	ProfileComplete bool
	ProfileStep     string
}

// AccountService resolves messenger identities and owns profile changes.
type AccountService interface {
	ResolveIdentity(context.Context, Identity) (Account, error)
	GetAccount(context.Context, int64) (Account, error)
	SaveProfile(context.Context, int64, string, string) (Account, error)
	ProfileInput(context.Context, int64, string, bool) (Account, error)
	GetDeliveryIdentities(context.Context, int64) ([]Identity, error)
}

// CleanProfileName accepts a nonempty name of at most 100 Unicode characters.
func CleanProfileName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 100 {
		return "", fmt.Errorf("%w: name must contain 1–100 characters", ErrValidation)
	}
	return value, nil
}

func (s *userServiceImpl) ResolveIdentity(ctx context.Context, identity Identity) (Account, error) {
	if (identity.Platform != "max" && identity.Platform != "telegram") || identity.ExternalID <= 0 {
		return Account{}, ErrValidation
	}
	id, err := s.querier.ResolveIdentity(ctx, db.ResolveIdentityParams{
		Platform: identity.Platform, ExternalID: identity.ExternalID, FirstName: identity.FirstName,
		LastName: identity.LastName, Username: identity.Username, IsBot: identity.IsBot, BotStarted: identity.BotStarted,
	})
	if err != nil {
		return Account{}, fmt.Errorf("resolve identity: %w", err)
	}
	return s.GetAccount(ctx, id)
}

func (s *userServiceImpl) GetAccount(ctx context.Context, id int64) (Account, error) {
	row, err := s.querier.GetAccount(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	user := contracts.User{ID: row.ID, FirstName: row.FirstName}
	if row.LastName.Valid {
		user.LastName.SetTo(row.LastName.String)
	}
	if row.Username.Valid {
		user.Username.SetTo(row.Username.String)
	}
	return Account{User: user, Platform: row.Platform, ExternalID: row.ExternalID, ProfileComplete: row.ProfileComplete, ProfileStep: row.ProfileStep}, nil
}

func (s *userServiceImpl) SaveProfile(ctx context.Context, id int64, first, last string) (Account, error) {
	first, err := CleanProfileName(first)
	if err != nil {
		return Account{}, err
	}
	last, err = CleanProfileName(last)
	if err != nil {
		return Account{}, err
	}
	if err = s.querier.SaveTelegramProfile(ctx, db.SaveTelegramProfileParams{UserID: id, FirstName: first, LastName: last}); err != nil {
		return Account{}, fmt.Errorf("save profile: %w", err)
	}
	return s.GetAccount(ctx, id)
}

func (s *userServiceImpl) ProfileInput(ctx context.Context, id int64, text string, restart bool) (Account, error) {
	if text != "" {
		var err error
		text, err = CleanProfileName(text)
		if err != nil {
			return Account{}, err
		}
	}
	if err := s.querier.TelegramProfileInput(ctx, db.TelegramProfileInputParams{UserID: id, Input: text, Restart: restart}); err != nil {
		return Account{}, fmt.Errorf("profile input: %w", err)
	}
	return s.GetAccount(ctx, id)
}

func (s *userServiceImpl) GetDeliveryIdentities(ctx context.Context, id int64) ([]Identity, error) {
	rows, err := s.querier.GetDeliveryIdentities(ctx, id)
	if err != nil {
		return nil, err
	}
	identities := make([]Identity, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, Identity{Platform: row.Platform, ExternalID: row.ExternalID})
	}
	return identities, nil
}
