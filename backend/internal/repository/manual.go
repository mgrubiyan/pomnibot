package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// ErrManualSetNotFound hides sets that the caller does not own.
var ErrManualSetNotFound = errors.New("set not found")

// TxBeginner is implemented by pgxpool.Pool.
type TxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// ManualStore persists each manual card and its fact in one transaction.
type ManualStore struct{ pool TxBeginner }

// NewManualStore uses the application's connection pool.
func NewManualStore(pool TxBeginner) *ManualStore { return &ManualStore{pool: pool} }

// CreateSet atomically creates a set, author membership and the first card.
func (s *ManualStore) CreateSet(ctx context.Context, userID int64, title string, req *contracts.CreateCardRequest) (uuid.UUID, error) {
	for range 20 {
		n, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return uuid.Nil, err
		}
		code := fmt.Sprintf("%06d", 100000+n.Int64())
		var setID uuid.UUID
		err = s.transaction(ctx, func(q *db.Queries) error {
			set, err := q.CreateSet(ctx, db.CreateSetParams{Title: title, AuthorID: userID, ShareCode: code})
			if err != nil {
				return err
			}
			setID = uuid.UUID(set.ID.Bytes)
			if _, err = q.JoinSet(ctx, db.JoinSetParams{UserID: userID, SetID: set.ID}); err != nil {
				return err
			}
			_, err = insertManualCard(ctx, q, set.ID, req)
			return err
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "sets_share_code_key" {
			continue
		}
		return setID, err
	}
	return uuid.Nil, fmt.Errorf("share code retry limit reached")
}

// CreateCard checks ownership while holding a lock until the card is committed.
func (s *ManualStore) CreateCard(ctx context.Context, userID int64, setID uuid.UUID, req *contracts.CreateCardRequest) (uuid.UUID, error) {
	var cardID uuid.UUID
	err := s.transaction(ctx, func(q *db.Queries) error {
		id := pgtype.UUID{Bytes: setID, Valid: true}
		if _, err := q.LockSetForAuthor(ctx, db.LockSetForAuthorParams{ID: id, AuthorID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrManualSetNotFound
			}
			return err
		}
		var err error
		cardID, err = insertManualCard(ctx, q, id, req)
		return err
	})
	return cardID, err
}

func (s *ManualStore) transaction(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(db.New(tx).WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertManualCard(ctx context.Context, q db.Querier, setID pgtype.UUID, req *contracts.CreateCardRequest) (uuid.UUID, error) {
	factID := uuid.NewString()
	if _, err := q.CreateFact(ctx, db.CreateFactParams{ID: factID, SetID: setID, Name: req.Question}); err != nil {
		return uuid.Nil, err
	}
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	answer := req.Answer.String
	if req.Answer.Type == contracts.StringArrayCardAnswer {
		data, _ := json.Marshal(req.Answer.StringArray)
		answer = string(data)
	}
	if req.Kind == contracts.CardKindBoolean {
		answer = strconv.FormatBool(req.Answer.Bool)
	}
	if _, err := q.CreateCard(ctx, db.CreateCardParams{ID: pgID, FactID: factID, Kind: string(req.Kind), Question: req.Question, AnswerText: pgtype.Text{String: answer, Valid: true}, Explanation: req.Explanation.Value}); err != nil {
		return uuid.Nil, err
	}
	if req.Kind == contracts.CardKindChoice {
		for i, v := range req.Options {
			if _, err := q.CreateCardOption(ctx, db.CreateCardOptionParams{CardID: pgID, Position: int32(i), Text: v, IsCorrect: v == answer || slices.Contains(req.Answer.StringArray, v)}); err != nil {
				return uuid.Nil, err
			}
		}
	}
	if req.Kind == contracts.CardKindTable {
		for i, v := range req.Table.Value.Columns {
			if _, err := q.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{CardID: pgID, Position: int32(i), Name: v}); err != nil {
				return uuid.Nil, err
			}
		}
		for i, v := range req.Table.Value.Items {
			if _, err := q.CreateCardTableItem(ctx, db.CreateCardTableItemParams{CardID: pgID, ItemText: v.Text, ColumnName: v.Column, Position: int32(i)}); err != nil {
				return uuid.Nil, err
			}
		}
	}
	return id, nil
}
