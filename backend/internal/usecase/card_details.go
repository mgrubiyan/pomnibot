package usecase

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// loadedCardDetails supplies ordered details for an already-authorized card list.
// Embed the real querier so the same card mapper can still serve single-card reads.
// This cache exists only for the duration of one request.
type loadedCardDetails struct {
	db.Querier
	options map[pgtype.UUID][]db.CardOption
	columns map[pgtype.UUID][]db.CardTableColumn
	items   map[pgtype.UUID][]db.CardTableItem
}

func loadCardDetails(ctx context.Context, q db.Querier, choiceIDs, tableIDs []pgtype.UUID) (db.Querier, error) {
	loaded := &loadedCardDetails{Querier: q, options: map[pgtype.UUID][]db.CardOption{}, columns: map[pgtype.UUID][]db.CardTableColumn{}, items: map[pgtype.UUID][]db.CardTableItem{}}
	if len(choiceIDs) > 0 {
		rows, err := q.GetCardOptionsBatch(ctx, choiceIDs)
		if err != nil {
			return nil, fmt.Errorf("get card options batch: %w", err)
		}
		for _, row := range rows {
			loaded.options[row.CardID] = append(loaded.options[row.CardID], row)
		}
	}
	if len(tableIDs) > 0 {
		columns, err := q.GetCardTableColumnsBatch(ctx, tableIDs)
		if err != nil {
			return nil, fmt.Errorf("get card table columns batch: %w", err)
		}
		for _, row := range columns {
			loaded.columns[row.CardID] = append(loaded.columns[row.CardID], row)
		}
		items, err := q.GetCardTableItemsBatch(ctx, tableIDs)
		if err != nil {
			return nil, fmt.Errorf("get card table items batch: %w", err)
		}
		for _, row := range items {
			loaded.items[row.CardID] = append(loaded.items[row.CardID], row)
		}
	}
	return loaded, nil
}

func (d *loadedCardDetails) GetCardOptions(_ context.Context, id pgtype.UUID) ([]db.CardOption, error) {
	return d.options[id], nil
}
func (d *loadedCardDetails) GetCardTableColumns(_ context.Context, id pgtype.UUID) ([]db.CardTableColumn, error) {
	return d.columns[id], nil
}
func (d *loadedCardDetails) GetCardTableItems(_ context.Context, id pgtype.UUID) ([]db.CardTableItem, error) {
	return d.items[id], nil
}

func choiceAnswer(options []db.CardOption, stored string) contracts.CardAnswer {
	var right []string
	for _, o := range options {
		if o.IsCorrect {
			right = append(right, o.Text)
		}
	}
	if len(right) == 0 {
		right = cards.ChoiceAnswers(stored)
	}
	return contracts.NewStringArrayCardAnswer(right)
}

func setCardIllustrations(card *contracts.Card, question, explanation pgtype.Text) {
	if question.Valid && question.String != "" {
		card.QuestionSvg.SetTo(question.String)
	}
	if explanation.Valid && explanation.String != "" {
		card.ExplanationSvg.SetTo(explanation.String)
	}
}
