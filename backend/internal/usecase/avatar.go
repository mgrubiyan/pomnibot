package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // users may send PNG
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // MAX may serve its photos as WebP
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
	// What image.Decode reads depends on the decoders linked anywhere in the
	// binary, so the formats are named here.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, fmt.Errorf("%w: photo must be JPEG or PNG", ErrValidation)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		return nil, fmt.Errorf("%w: photo must be at most %d px a side", ErrValidation, maxAvatarSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: photo is damaged", ErrValidation)
	}
	// JPEG has no transparency: what was see-through becomes white, not
	// black. An opaque photo, such as every JPEG, is encoded as it is.
	if o, ok := img.(interface{ Opaque() bool }); !ok || !o.Opaque() {
		flat := image.NewRGBA(img.Bounds())
		draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		img = flat
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode photo: %w", err)
	}
	return out.Bytes(), nil
}

const (
	// A messenger photo is cut square to this side, the size the mini-app
	// itself used for an uploaded one.
	importedAvatarSide = 512
	// Checked from the header before decoding: a photo URL is the
	// messenger's, but the file behind it is still a stranger's.
	maxImportedAvatarPixels = 25_000_000
)

// ImportAvatar takes the photo from the user's messenger profile: cut to a
// centred square, scaled down and saved as JPEG. source is the address it
// came from, so the next sign-in with the same one does not fetch it again.
func (s *userServiceImpl) ImportAvatar(ctx context.Context, id uuid.UUID, source string, data []byte) (Account, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return Account{}, fmt.Errorf("%w: messenger photo is not JPEG, PNG or WebP", ErrValidation)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > maxImportedAvatarPixels {
		return Account{}, fmt.Errorf("%w: messenger photo is %dx%d", ErrValidation, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Account{}, fmt.Errorf("%w: messenger photo is damaged", ErrValidation)
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, side, side).Add(image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2))
	out := min(side, importedAvatarSide)
	square := image.NewRGBA(image.Rect(0, 0, out, out))
	// JPEG has no transparency: what was see-through becomes white.
	draw.Draw(square, square.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(square, square.Bounds(), img, crop, xdraw.Over, nil)
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, square, &jpeg.Options{Quality: 85}); err != nil {
		return Account{}, fmt.Errorf("encode messenger photo: %w", err)
	}
	row, err := s.querier.SaveImportedAvatar(ctx, db.SaveImportedAvatarParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, Image: photo.Bytes(),
		Source: pgtype.Text{String: strings.TrimSpace(source), Valid: true},
	})
	if err != nil {
		return Account{}, fmt.Errorf("save messenger photo: %w", err)
	}
	return accountFromUser(row), nil
}

// ForgetImportedAvatar removes the photo taken from the messenger once the
// profile there has none; a photo the user uploaded stays.
func (s *userServiceImpl) ForgetImportedAvatar(ctx context.Context, id uuid.UUID) error {
	if err := s.querier.ForgetImportedAvatar(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		return fmt.Errorf("forget messenger photo: %w", err)
	}
	return nil
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
