package perftest

import (
	"bytes"
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
)

func verify(ctx context.Context, pool *pgxpool.Pool, w *workload) error {
	seen := map[uuid.UUID]bool{}
	for _, a := range w.actors {
		if a.id == uuid.Nil || seen[a.id] {
			return fmt.Errorf("non-distinct identity %s", a.id)
		}
		seen[a.id] = true
		var external int64
		column := "max_id"
		if a.index%2 == 1 {
			column = "tg_id"
		}
		if err := pool.QueryRow(ctx, "SELECT "+column+" FROM users WHERE id=$1", a.id).Scan(&external); err != nil {
			return err
		}
		if external != int64(10000000+a.index/2) {
			return fmt.Errorf("wrong external identity: %d", external)
		}
		var membership, answers int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_sets WHERE user_id=$1", a.id).Scan(&membership); err != nil {
			return err
		}
		if membership != 3 {
			return fmt.Errorf("user %s memberships=%d", a.id, membership)
		}
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM answers WHERE user_id=$1", a.id).Scan(&answers); err != nil {
			return err
		}
		if answers != a.submitted.Load() {
			return fmt.Errorf("user %s answers=%d expected=%d", a.id, answers, a.submitted.Load())
		}
		var bad int
		// Independent expected progress from persisted answers, checked per fact.
		if err := pool.QueryRow(ctx, `WITH expected AS (SELECT c.fact_id,count(*) n FROM answers a JOIN cards c ON c.id=a.card_id WHERE a.user_id=$1 GROUP BY c.fact_id)
 SELECT count(*) FROM expected e LEFT JOIN user_fact_progress p ON p.user_id=$1 AND p.fact_id=e.fact_id WHERE p.fact_id IS NULL OR p.last_reviewed_at IS NULL`, a.id).Scan(&bad); err != nil {
			return err
		}
		if bad != 0 {
			return fmt.Errorf("user %s incorrect progress for %d facts", a.id, bad)
		}
	}
	var total int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM answers WHERE user_id IN (SELECT id FROM users WHERE first_name='Background')").Scan(&total); err != nil {
		return err
	}
	if total != 100000 {
		return fmt.Errorf("history changed: %d", total)
	}
	var board contracts.SetLeaderboardResponse
	if err := w.request(ctx, nil, w.owner, "leaderboard", "GET", "/sets/"+setID(1)+"/leaderboard", nil, &board); err != nil {
		return err
	}
	if len(board.Items) != 1001+len(w.actors) {
		return fmt.Errorf("leaderboard participants=%d expected=%d", len(board.Items), 1001+len(w.actors))
	}
	// Independent score calculation, sort/rank in Go rather than the ranking SQL.
	scores := map[uuid.UUID]float64{}
	rows, err := pool.Query(ctx, `SELECT us.user_id, COALESCE(sum(p.step::float / 5.0), 0) FROM user_sets us LEFT JOIN user_fact_progress p ON p.user_id=us.user_id AND p.fact_id IN (SELECT id FROM facts WHERE set_id=$1) WHERE us.set_id=$1 GROUP BY us.user_id`, setID(1))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var sum float64
		if err = rows.Scan(&id, &sum); err != nil {
			rows.Close()
			return err
		}
		scores[id] = sum / 50
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, item := range board.Items {
		avg, ok := scores[item.User.ID]
		if !ok {
			return fmt.Errorf("unexpected leaderboard user %s", item.User.ID)
		}
		rank := 1
		higher := map[float64]bool{}
		for _, score := range scores {
			if score > avg {
				higher[score] = true
			}
		}
		rank += len(higher)
		percentile := int(math.Round(avg * 100))
		percentile = max(0, min(100, percentile))
		if item.Rank != rank || item.Percentile != percentile {
			return fmt.Errorf("leaderboard user %s got rank=%d percentile=%d want %d/%d", item.User.ID, item.Rank, item.Percentile, rank, percentile)
		}
		if i > 0 {
			previous := board.Items[i-1]
			if previous.Rank > item.Rank || (previous.Rank == item.Rank && bytes.Compare(previous.User.ID[:], item.User.ID[:]) >= 0) {
				return fmt.Errorf("leaderboard ordering or duplicate")
			}
		}
	}
	return nil
}
