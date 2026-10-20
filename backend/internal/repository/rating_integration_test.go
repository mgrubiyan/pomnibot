package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// Scores must include the default 700 for missing facts, exclude other sets,
// include authors without membership rows, and preserve ties and empty sets.
func TestSetRatingPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO users(id,first_name) VALUES(11,'Author'),(22,'Partial'),(33,'Full'),(44,'Tied'),(55,'Outsider'),(66,'Low')`,
		`INSERT INTO sets(id,title,author_id,share_code) VALUES('00000000-0000-0000-0000-000000000001','Shared',11,'123456'),('00000000-0000-0000-0000-000000000002','Empty',11,'234567'),('00000000-0000-0000-0000-000000000003','Other',11,'345678')`,
		`INSERT INTO user_sets(user_id,set_id) SELECT id,'00000000-0000-0000-0000-000000000001'::uuid FROM users WHERE id IN (22,33,44,66)`,
		`INSERT INTO facts(id,set_id,name) VALUES('a','00000000-0000-0000-0000-000000000001','A'),('b','00000000-0000-0000-0000-000000000001','B'),('other','00000000-0000-0000-0000-000000000003','Other')`,
		`INSERT INTO user_fact_progress(user_id,fact_id,elo) VALUES(22,'a',1200),(22,'other',9999),(33,'a',1000),(33,'b',1000),(44,'a',1200),(55,'a',1400),(55,'b',1400),(66,'a',400)`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	q := db.New(pool)
	shared := pgtype.UUID{Bytes: [16]byte{15: 1}, Valid: true}
	board, err := q.GetSetLeaderboard(ctx, shared)
	if err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		id               int64
		rank, percentile int32
	}{{33, 1, 33}, {22, 2, 25}, {44, 2, 25}, {11, 4, 0}, {66, 5, 0}}
	if len(board) != len(expected) {
		t.Fatalf("participants: %+v", board)
	}
	for i, want := range expected {
		got := board[i]
		if got.UserID != want.id || got.Rank != want.rank || got.Percentile != want.percentile {
			t.Fatalf("row %d: %+v want %+v", i, got, want)
		}
		rating, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: shared, UserID: want.id})
		if err != nil || rating.Rank != want.rank || rating.Percentile != want.percentile {
			t.Fatalf("rating %d: %+v %v", want.id, rating, err)
		}
	}
	// GetUserSetRating includes its requested user even without membership.
	outsider, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: shared, UserID: 55})
	if err != nil || outsider.Rank != 1 || outsider.Percentile != 100 {
		t.Fatalf("outsider: %+v %v", outsider, err)
	}
	empty := pgtype.UUID{Bytes: [16]byte{15: 2}, Valid: true}
	emptyBoard, err := q.GetSetLeaderboard(ctx, empty)
	if err != nil || len(emptyBoard) != 1 || emptyBoard[0].UserID != 11 || emptyBoard[0].Rank != 1 || emptyBoard[0].Percentile != 0 {
		t.Fatalf("empty leaderboard: %+v %v", emptyBoard, err)
	}
	emptyRating, err := q.GetUserSetRating(ctx, db.GetUserSetRatingParams{SetID: empty, UserID: 55})
	if err != nil || emptyRating.Rank != 1 || emptyRating.Percentile != 0 {
		t.Fatalf("empty rating: %+v %v", emptyRating, err)
	}
}
