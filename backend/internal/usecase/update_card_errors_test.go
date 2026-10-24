package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// UpdateCard rewrites a card's options and table layout row by row. A failed
// row used to be dropped silently and the card reported as saved.
func TestCardService_UpdateCard_ReportsFailedRows(t *testing.T) {
	errDB := errors.New("connection reset")

	tests := []struct {
		name string
		mock func(m *mockQuerier)
	}{
		{"delete options", func(m *mockQuerier) {
			m.deleteCardOptionsFunc = func(context.Context, pgtype.UUID) error { return errDB }
		}},
		{"create option", func(m *mockQuerier) {
			m.createCardOptionFunc = func(context.Context, db.CreateCardOptionParams) (db.CardOption, error) {
				return db.CardOption{}, errDB
			}
		}},
		{"delete table columns", func(m *mockQuerier) {
			m.deleteCardTableColumnsFunc = func(context.Context, pgtype.UUID) error { return errDB }
		}},
		{"delete table items", func(m *mockQuerier) {
			m.deleteCardTableItemsFunc = func(context.Context, pgtype.UUID) error { return errDB }
		}},
		{"create table column", func(m *mockQuerier) {
			m.createCardTableColumnFunc = func(context.Context, db.CreateCardTableColumnParams) (db.CardTableColumn, error) {
				return db.CardTableColumn{}, errDB
			}
		}},
		{"create table item", func(m *mockQuerier) {
			m.createCardTableItemFunc = func(context.Context, db.CreateCardTableItemParams) (db.CardTableItem, error) {
				return db.CardTableItem{}, errDB
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockQuerier{
				updateCardFunc: func(_ context.Context, arg db.UpdateCardParams) (db.UpdateCardRow, error) {
					return db.UpdateCardRow{ID: arg.ID, Kind: arg.Kind, Question: arg.Question, AnswerText: arg.AnswerText}, nil
				},
			}
			tt.mock(m)
			svc := usecase.NewCardService(m, usecase.NewUserService(m))

			req := &contracts.UpdateCardRequest{
				Options: []string{"Анафаза", "Профаза"},
				Table: contracts.NewOptTableLayout(contracts.TableLayout{
					Columns: []string{"Фаза"},
					Items:   []contracts.TableItem{{Text: "Анафаза", Column: "Фаза"}},
				}),
			}
			res, err := svc.UpdateCard(context.Background(), uuid.New(), uuid.New(), req)
			if !errors.Is(err, errDB) {
				t.Fatalf("err = %v, want it to wrap %v", err, errDB)
			}
			if res != nil {
				t.Errorf("res = %+v, want nil when the card was not fully saved", res)
			}
		})
	}
}
