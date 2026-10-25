package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // users may send PNG

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

const (
	// The mini-app sends a 512 px JPEG; the limits leave room
	// for other clients and keep a decoded photo within a few megabytes.
	maxAvatarBytes = 300 << 10
	maxAvatarSide  = 1024
)

// CleanAvatar checks a photo and encodes it again as JPEG, which leaves
// behind whatever else the file carried, such as where it was taken.
func CleanAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxAvatarBytes {
		return nil, fmt.Errorf("%w: photo must be at most 300 KB", ErrValidation)
	}
	// The header tells the size before any pixel is allocated.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: photo must be JPEG or PNG", ErrValidation)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		return nil, fmt.Errorf("%w: photo must be at most %d px a side", ErrValidation, maxAvatarSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: photo is damaged", ErrValidation)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode photo: %w", err)
	}
	return out.Bytes(), nil
}

// AvatarURL is where the photo is served; empty without one.
func AvatarURL(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return "/api/avatars/" + uuid.UUID(id.Bytes).String()
}

func setAvatar(u *contracts.User, id pgtype.UUID) {
	if url := AvatarURL(id); url != "" {
		u.AvatarUrl.SetTo(url)
	}
}

func (s *userServiceImpl) SaveAvatar(ctx context.Context, id uuid.UUID, data []byte) (Account, error) {
	photo, err := CleanAvatar(data)
	if err != nil {
		return Account{}, err
	}
	row, err := s.querier.SaveAvatar(ctx, db.SaveAvatarParams{ID: pgtype.UUID{Bytes: id, Valid: true}, Image: photo})
	if err != nil {
		return Account{}, fmt.Errorf("save avatar: %w", err)
	}
	return accountFromUser(row), nil
}

func (s *userServiceImpl) DeleteAvatar(ctx context.Context, id uuid.UUID) (Account, error) {
	row, err := s.querier.DeleteAvatar(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return Account{}, fmt.Errorf("delete avatar: %w", err)
	}
	return accountFromUser(row), nil
}

// AvatarImage returns a stored photo, or ErrNotFound.
func (s *userServiceImpl) AvatarImage(ctx context.Context, avatarID uuid.UUID) ([]byte, error) {
	photo, err := s.querier.GetAvatarImage(ctx, pgtype.UUID{Bytes: avatarID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get avatar: %w", err)
	}
	return photo, nil
}
