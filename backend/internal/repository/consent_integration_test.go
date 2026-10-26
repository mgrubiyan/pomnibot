//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// The first visit asks for consent until the user answers either way; a
// later change in the profile keeps the time of that first answer.
func TestDataConsentAnsweredPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	ann := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000a2"), Valid: true}
	bob := pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-0000-0000-0000000000b2"), Valid: true}
	for _, id := range []pgtype.UUID{ann, bob} {
		user, err := q.EnsureUser(ctx, id)
		if err != nil || user.DataConsentAnsweredAt.Valid {
			t.Fatalf("new user answered: %+v %v", user, err)
		}
	}

	agreed, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: ann, Given: true})
	if err != nil || !agreed.DataConsentAt.Valid || !agreed.DataConsentAnsweredAt.Valid {
		t.Fatalf("agree: %+v %v", agreed, err)
	}
	declined, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: bob, Given: false})
	if err != nil || declined.DataConsentAt.Valid || !declined.DataConsentAnsweredAt.Valid {
		t.Fatalf("decline: %+v %v", declined, err)
	}

	withdrawn, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: ann, Given: false})
	if err != nil || withdrawn.DataConsentAt.Valid || withdrawn.DataConsentAnsweredAt != agreed.DataConsentAnsweredAt {
		t.Fatalf("withdraw: %+v → %+v, %v", agreed.DataConsentAnsweredAt, withdrawn.DataConsentAnsweredAt, err)
	}
	again, err := q.SetDataConsent(ctx, db.SetDataConsentParams{ID: bob, Given: true})
	if err != nil || !again.DataConsentAt.Valid || again.DataConsentAnsweredAt != declined.DataConsentAnsweredAt {
		t.Fatalf("agree later: %+v → %+v, %v", declined.DataConsentAnsweredAt, again.DataConsentAnsweredAt, err)
	}
}
