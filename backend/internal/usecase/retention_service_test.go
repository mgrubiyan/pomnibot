package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type retentionQuerier struct {
	db.Querier
	getSet   func(context.Context, db.GetSetByIDParams) (db.GetSetByIDRow, error)
	getFacts func(context.Context, db.GetSetRetentionFactsParams) ([]db.GetSetRetentionFactsRow, error)
}

func (q retentionQuerier) GetSetByID(ctx context.Context, p db.GetSetByIDParams) (db.GetSetByIDRow, error) {
	return q.getSet(ctx, p)
}
func (q retentionQuerier) GetSetRetentionFacts(ctx context.Context, p db.GetSetRetentionFactsParams) ([]db.GetSetRetentionFactsRow, error) {
	return q.getFacts(ctx, p)
}

func TestRetentionServiceAccessAndErrors(t *testing.T) {
	owner, member, stranger, setID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	dbError := errors.New("database unavailable")
	for _, tc := range []struct {
		name                         string
		user                         uuid.UUID
		accessErr, factsErr, wantErr error
	}{
		{name: "author", user: owner}, {name: "member", user: member},
		{name: "stranger", user: stranger, accessErr: pgx.ErrNoRows, wantErr: ErrNotFound},
		{name: "missing", user: owner, accessErr: pgx.ErrNoRows, wantErr: ErrNotFound},
		{name: "access database error", user: owner, accessErr: dbError, wantErr: dbError},
		{name: "facts database error", user: owner, factsErr: dbError, wantErr: dbError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []db.GetSetRetentionFactsRow{{Available: true}, {Available: false}}
			before := append([]db.GetSetRetentionFactsRow(nil), rows...)
			q := retentionQuerier{
				getSet: func(_ context.Context, p db.GetSetByIDParams) (db.GetSetByIDRow, error) {
					if p.UserID.Bytes != tc.user || p.ID.Bytes != setID {
						t.Fatal("wrong access parameters")
					}
					return db.GetSetByIDRow{}, tc.accessErr
				},
				getFacts: func(_ context.Context, p db.GetSetRetentionFactsParams) ([]db.GetSetRetentionFactsRow, error) {
					if tc.accessErr != nil {
						t.Fatal("facts queried after access failure")
					}
					if p.UserID.Bytes != tc.user || p.SetID.Bytes != setID {
						t.Fatal("wrong fact parameters")
					}
					return rows, tc.factsErr
				},
			}
			result, err := NewSetService(q, nil).GetSetRetention(t.Context(), tc.user, setID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v want=%v", err, tc.wantErr)
			}
			if tc.wantErr == nil && (result.TotalFacts != 2 || result.Current != 0 || result.CalculatedAt.IsZero() || result.HorizonDays != 13 || result.Target != 0.9) {
				t.Fatalf("response: %+v", result)
			}
			if !reflect.DeepEqual(before, rows) {
				t.Fatal("stored rows mutated")
			}
			// All other db.Querier methods are nil: any attempted write would panic.
		})
	}
}
