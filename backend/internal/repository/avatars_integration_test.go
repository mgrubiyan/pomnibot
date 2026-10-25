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
