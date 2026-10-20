package usecase_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type detailBatchQuerier struct {
	mockQuerier
	batches int
	singles int
}

func (q *detailBatchQuerier) GetCardOptions(_ context.Context, id pgtype.UUID) ([]db.CardOption, error) {
	q.singles++
	return []db.CardOption{{CardID: id, Text: "Answer"}}, nil
}
func (q *detailBatchQuerier) GetCardTableColumns(_ context.Context, id pgtype.UUID) ([]db.CardTableColumn, error) {
	q.singles++
	return []db.CardTableColumn{{CardID: id, Name: "A"}}, nil
}
func (q *detailBatchQuerier) GetCardTableItems(_ context.Context, id pgtype.UUID) ([]db.CardTableItem, error) {
	q.singles++
	return []db.CardTableItem{{CardID: id, ItemText: "Item", ColumnName: "A"}}, nil
}
func (q *detailBatchQuerier) GetCardOptionsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardOption, error) {
	q.batches++
	rows := []db.CardOption{}
	for _, id := range ids {
		rows = append(rows, db.CardOption{CardID: id, Text: "Answer"})
	}
	return rows, nil
}
func (q *detailBatchQuerier) GetCardTableColumnsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardTableColumn, error) {
	q.batches++
	rows := []db.CardTableColumn{}
	for _, id := range ids {
		rows = append(rows, db.CardTableColumn{CardID: id, Name: "A"})
	}
	return rows, nil
}
func (q *detailBatchQuerier) GetCardTableItemsBatch(_ context.Context, ids []pgtype.UUID) ([]db.CardTableItem, error) {
	q.batches++
	rows := []db.CardTableItem{}
	for _, id := range ids {
		rows = append(rows, db.CardTableItem{CardID: id, ItemText: "Item", ColumnName: "A"})
	}
	return rows, nil
}

func TestCardListsBatchDetails(t *testing.T) {
	for _, feed := range []bool{false, true} {
		t.Run(fmt.Sprintf("feed=%v", feed), func(t *testing.T) {
			q := &detailBatchQuerier{}
			set := pgtype.UUID{Bytes: [16]byte{15: 1}, Valid: true}
			q.getCardsBySetIDFunc = func(context.Context, db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error) {
				rows := []db.GetCardsBySetIDRow{}
				for i := 0; i < 20; i++ {
					kind := "choice"
					if i%2 == 1 {
						kind = "table"
					}
					rows = append(rows, db.GetCardsBySetIDRow{ID: pgtype.UUID{Bytes: [16]byte{15: byte(i + 2)}, Valid: true}, SetID: set, Kind: kind, AnswerText: pgtype.Text{String: "Answer", Valid: true}})
				}
				return rows, nil
			}
			q.getFeedCardsForUserFunc = func(ctx context.Context, _ int64) ([]db.GetFeedCardsForUserRow, error) {
				source, _ := q.getCardsBySetIDFunc(ctx, db.GetCardsBySetIDParams{})
				rows := []db.GetFeedCardsForUserRow{}
				for _, r := range source {
					rows = append(rows, db.GetFeedCardsForUserRow{ID: r.ID, SetID: r.SetID, Kind: r.Kind, AnswerText: r.AnswerText})
				}
				return rows, nil
			}
			users := usecase.NewUserService(q)
			var count int
			if feed {
				cards, err := usecase.NewHomescreenService(q, users).GetFeedQuestions(context.Background(), 22)
				if err != nil {
					t.Fatal(err)
				}
				count = len(cards)
				for i, c := range cards {
					if i%2 == 0 && (len(c.Options) != 1 || c.Options[0] != "Answer") {
						t.Fatalf("lost choice details: %+v", c)
					}
					if i%2 == 1 && (len(c.Table.Value.Columns) != 1 || len(c.Table.Value.Items) != 1) {
						t.Fatalf("lost table details: %+v", c)
					}
				}
			} else {
				cards, err := usecase.NewSetService(q, users).GetCardsBySetID(context.Background(), 22, uuid.UUID(set.Bytes))
				if err != nil {
					t.Fatal(err)
				}
				count = len(cards)
				for i, c := range cards {
					if i%2 == 0 && (len(c.Options) != 1 || c.Options[0] != "Answer") {
						t.Fatalf("lost choice details: %+v", c)
					}
					if i%2 == 1 && (len(c.Table.Value.Columns) != 1 || len(c.Table.Value.Items) != 1) {
						t.Fatalf("lost table details: %+v", c)
					}
				}
			}
			if count != 20 || q.singles != 0 || q.batches != 3 {
				t.Fatalf("cards=%d per-card queries=%d batch queries=%d; expected 20/0/3", count, q.singles, q.batches)
			}
		})
	}
}
