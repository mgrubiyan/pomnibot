package repository_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// A new photo replaces the old one under a new address, and removing it
// leaves no row behind.
func TestAvatarsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	ann := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"), Valid: true}
	bob := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000b1"), Valid: true}
	for _, id := range []pgtype.UUID{ann, bob} {
		if _, err := q.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	count := func() (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM avatars").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	first, err := q.SaveAvatar(ctx, db.SaveAvatarParams{ID: ann, Image: []byte("first")})
	if err != nil || !first.AvatarID.Valid {
		t.Fatalf("first photo: %+v %v", first, err)
	}
	if _, err := q.SaveAvatar(ctx, db.SaveAvatarParams{ID: bob, Image: []byte("bob")}); err != nil {
		t.Fatal(err)
	}
	second, err := q.SaveAvatar(ctx, db.SaveAvatarParams{ID: ann, Image: []byte("second")})
	if err != nil || !second.AvatarID.Valid || second.AvatarID == first.AvatarID {
		t.Fatalf("second photo kept the address: %v → %v, %v", first.AvatarID, second.AvatarID, err)
	}
	if n := count(); n != 2 {
		t.Fatalf("%d photos, want 2", n)
	}
	if _, err := q.GetAvatarImage(ctx, first.AvatarID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old photo still served: %v", err)
	}
	if image, err := q.GetAvatarImage(ctx, second.AvatarID); err != nil || !bytes.Equal(image, []byte("second")) {
		t.Fatalf("new photo: %q %v", image, err)
	}

	// Withdrawing consent moves the photo to a new address: authors may
	// still hold the old one.
	agreed, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: ann, Given: true})
	if err != nil || agreed.AvatarID != second.AvatarID {
		t.Fatalf("consent moved the photo: %v → %v, %v", second.AvatarID, agreed.AvatarID, err)
	}
	withdrawn, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: ann, Given: false})
	if err != nil || !withdrawn.AvatarID.Valid || withdrawn.AvatarID == second.AvatarID {
		t.Fatalf("photo kept its address after withdrawal: %v → %v, %v", second.AvatarID, withdrawn.AvatarID, err)
	}
	if _, err := q.GetAvatarImage(ctx, second.AvatarID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old address still served after withdrawal: %v", err)
	}
	if image, err := q.GetAvatarImage(ctx, withdrawn.AvatarID); err != nil || !bytes.Equal(image, []byte("second")) {
		t.Fatalf("photo lost on withdrawal: %q %v", image, err)
	}
	second = withdrawn
	nobody, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: bob, Given: false})
	if err != nil || !nobody.AvatarID.Valid {
		t.Fatalf("withdrawal without consent: %+v %v", nobody, err)
	}

	removed, err := q.DeleteAvatar(ctx, ann)
	if err != nil || removed.AvatarID.Valid {
		t.Fatalf("delete: %+v %v", removed, err)
	}
	if n := count(); n != 1 {
		t.Fatalf("%d photos after delete, want only Bob's", n)
	}
	if _, err := q.GetAvatarImage(ctx, second.AvatarID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted photo still served: %v", err)
	}
}

// A messenger photo remembers where it came from; once the messenger has
// none it goes, while a photo the user uploaded stays.
func TestImportedAvatarsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	ann := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000a2"), Valid: true}
	bob := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000b2"), Valid: true}
	for _, id := range []pgtype.UUID{ann, bob} {
		if _, err := q.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	source := pgtype.Text{String: "https://i.max.ru/ann.jpg", Valid: true}
	imported, err := q.SaveImportedAvatar(ctx, db.SaveImportedAvatarParams{ID: ann, Image: []byte("ann"), Source: source})
	if err != nil || !imported.AvatarID.Valid || imported.AvatarSource != source {
		t.Fatalf("imported: %+v %v", imported, err)
	}
	// Bob's messenger photo is replaced by one he uploads: that one is his own.
	if _, err := q.SaveImportedAvatar(ctx, db.SaveImportedAvatarParams{ID: bob, Image: []byte("bob tg"), Source: source}); err != nil {
		t.Fatal(err)
	}
	uploaded, err := q.SaveAvatar(ctx, db.SaveAvatarParams{ID: bob, Image: []byte("bob")})
	if err != nil || uploaded.AvatarSource.Valid {
		t.Fatalf("uploaded photo still counts as the messenger's: %+v %v", uploaded, err)
	}
	for _, id := range []pgtype.UUID{ann, bob} {
		if err := q.ForgetImportedAvatar(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if user, err := q.GetUserByID(ctx, ann); err != nil || user.AvatarID.Valid || user.AvatarSource.Valid {
		t.Fatalf("messenger photo kept: %+v %v", user, err)
	}
	if _, err := q.GetAvatarImage(ctx, imported.AvatarID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("messenger photo still served: %v", err)
	}
	if user, err := q.GetUserByID(ctx, bob); err != nil || user.AvatarID != uploaded.AvatarID {
		t.Fatalf("uploaded photo lost: %+v %v", user, err)
	}
}
