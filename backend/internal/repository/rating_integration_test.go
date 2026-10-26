//go:build integration

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// Scores must calculate FSRS-6 mastery index correctly, exclude other sets,
// include authors without membership rows, and preserve ties and empty sets.
func TestSetRatingPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	u11 := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	u22 := uuid.MustParse("00000000-0000-0000-0000-000000000022")
	u33 := uuid.MustParse("00000000-0000-0000-0000-000000000033")
	u44 := uuid.MustParse("00000000-0000-0000-0000-000000000044")
	u55 := uuid.MustParse("00000000-0000-0000-0000-000000000055")
	u66 := uuid.MustParse("00000000-0000-0000-0000-000000000066")

	s1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	s2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	s3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	fa := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	fb := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	fo := uuid.MustParse("00000000-0000-0000-0000-00000000000c")

	for _, sql := range []string{
		fmt.Sprintf(`INSERT INTO users(id,first_name) VALUES('%s','Author'),('%s','Partial'),('%s','Full'),('%s','Tied'),('%s','Outsider'),('%s','Low')`, u11, u22, u33, u44, u55, u66),
		fmt.Sprintf(`INSERT INTO sets(id,title,author_id,share_code) VALUES('%s','Shared','%s','123456'),('%s','Empty','%s','234567'),('%s','Other','%s','345678')`, s1, u11, s2, u11, s3, u11),
		fmt.Sprintf(`INSERT INTO user_sets(user_id,set_id) SELECT id,'%s'::uuid FROM users WHERE id IN ('%s','%s','%s','%s')`, s1, u22, u33, u44, u66),
		fmt.Sprintf(`INSERT INTO facts(id,set_id,name) VALUES('%s','%s','A'),('%s','%s','B'),('%s','%s','Other')`, fa, s1, fb, s1, fo, s3),
		fmt.Sprintf(`INSERT INTO cards(id,fact_id,kind,question,answer_text,explanation,source_quote) VALUES
			(uuidv7(),'%s','choice','QA','AA','EA','QA'),
			(uuidv7(),'%s','choice','QB','AB','EB','QB'),
			(uuidv7(),'%s','choice','QO','AO','EO','QO')`, fa, fb, fo),
		fmt.Sprintf(`INSERT INTO user_fact_progress(user_id,fact_id,step,stability,difficulty,last_reviewed_at) VALUES
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',1,10.0,5.0,NOW()),
			('%s','%s',0,1.0,7.0,NOW())`,
			u22, fa, u22, fo, u33, fa, u33, fb, u44, fa, u55, fa, u55, fb, u66, fa),
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	q := db.New(pool)
	shared := pgtype.UUID{Bytes: s1, Valid: true}
	board, err := q.GetSetLeaderboard(ctx, shared)
	if err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		id               uuid.UUID
		rank, percentile int32
	}{{u33, 1, 100}, {u22, 2, 50}, {u44, 2, 50}, {u11, 3, 0}, {u66, 3, 0}}
	if len(board) != len(expected) {
		t.Fatalf("participants: %+v", board)
	}
	for i, want := range expected {
		got := board[i]
		if got.UserID.Bytes != want.id || got.Rank != want.rank || got.Percentile != want.percentile {
			t.Fatalf("row %d: %+v want %+v", i, got, want)
		}
		rating, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: shared, UserID: pgtype.UUID{Bytes: want.id, Valid: true}})
		if err != nil || rating.Rank != want.rank || rating.Percentile != want.percentile {
			t.Fatalf("rating %v: %+v %v", want.id, rating, err)
		}
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE users SET data_consent_at = NOW() WHERE id = '%s'`, u22)); err != nil {
		t.Fatal(err)
	}
	board, err = q.GetSetLeaderboard(ctx, shared)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range board {
		if row.DataConsent != (row.UserID.Bytes == u22) {
			t.Fatalf("consent of %v: %v", row.UserID, row.DataConsent)
		}
	}
	// GetUserSetRating includes its requested user even without membership.
	outsider, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: shared, UserID: pgtype.UUID{Bytes: u55, Valid: true}})
	if err != nil || outsider.Rank != 1 || outsider.Percentile != 100 {
		t.Fatalf("outsider: %+v %v", outsider, err)
	}
	empty := pgtype.UUID{Bytes: s2, Valid: true}
	emptyBoard, err := q.GetSetLeaderboard(ctx, empty)
	if err != nil || len(emptyBoard) != 1 || emptyBoard[0].UserID.Bytes != u11 || emptyBoard[0].Rank != 1 || emptyBoard[0].Percentile != 0 {
		t.Fatalf("empty leaderboard: %+v %v", emptyBoard, err)
	}
	emptyRating, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: empty, UserID: pgtype.UUID{Bytes: u55, Valid: true}})
	if err != nil || emptyRating.Rank != 1 || emptyRating.Percentile != 0 {
		t.Fatalf("empty rating: %+v %v", emptyRating, err)
	}
}
