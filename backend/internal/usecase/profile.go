package usecase

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// CleanProfileName accepts a nonempty name of at most 100 Unicode characters.
func CleanProfileName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 100 {
		return "", fmt.Errorf("%w: name must contain 1–100 characters", ErrValidation)
	}
	return value, nil
}

func (s *userServiceImpl) SaveProfile(ctx context.Context, id uuid.UUID, first, last string) (Account, error) {
	first, err := CleanProfileName(first)
	if err != nil {
		return Account{}, err
	}
	last, err = CleanProfileName(last)
	if err != nil {
		return Account{}, err
	}
	var pgID pgtype.UUID
	_ = pgID.Scan(id.String())
	row, err := s.querier.SaveTelegramProfile(ctx, db.SaveTelegramProfileParams{ID: pgID, FirstName: first, LastName: pgtype.Text{String: last, Valid: true}})
	if err != nil {
		return Account{}, fmt.Errorf("save profile: %w", err)
	}
	return accountFromUser(row), nil
}

func (s *userServiceImpl) ProfileInput(ctx context.Context, id uuid.UUID, text string, restart bool) (Account, error) {
	if text != "" {
		var err error
		text, err = CleanProfileName(text)
		if err != nil {
			return Account{}, err
		}
	}
	var pgID pgtype.UUID
	_ = pgID.Scan(id.String())
	row, err := s.querier.TelegramProfileInput(ctx, db.TelegramProfileInputParams{ID: pgID, Input: text, Restart: restart})
	if err != nil {
		return Account{}, fmt.Errorf("profile input: %w", err)
	}
	return accountFromUser(row), nil
}
