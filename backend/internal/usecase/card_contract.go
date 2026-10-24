package usecase

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// storedCard is a card as the card queries read it; their rows share these
// fields, so each converts to it.
type storedCard = db.GetCardByIDRow

// cardDetails are the options of a choice card and the layout of a table
// card; empty for the other kinds.
type cardDetails struct {
	options []db.CardOption
	table   contracts.TableLayout
}

func loadDetails(ctx context.Context, q db.Querier, c storedCard) (cardDetails, error) {
	var d cardDetails
	switch contracts.CardKind(c.Kind) {
	case contracts.CardKindChoice:
		opts, err := q.GetCardOptions(ctx, c.ID)
		if err != nil {
			return d, fmt.Errorf("get card options: %w", err)
		}
		d.options = opts
	case contracts.CardKindTable:
		cols, err := q.GetCardTableColumns(ctx, c.ID)
		if err != nil {
			return d, fmt.Errorf("get card table columns: %w", err)
		}
		items, err := q.GetCardTableItems(ctx, c.ID)
		if err != nil {
			return d, fmt.Errorf("get card table items: %w", err)
		}
		d.table = contracts.TableLayout{
			Columns: make([]string, 0, len(cols)),
			Items:   make([]contracts.TableItem, 0, len(items)),
		}
		for _, col := range cols {
			d.table.Columns = append(d.table.Columns, col.Name)
		}
		for _, item := range items {
			d.table.Items = append(d.table.Items, contracts.TableItem{Text: item.ItemText, Column: item.ColumnName})
		}
	}
	return d, nil
}

func (d cardDetails) optionTexts() []string {
	texts := make([]string, 0, len(d.options))
	for _, o := range d.options {
		texts = append(texts, o.Text)
	}
	return texts
}

type cardIDs struct {
	card, set, fact uuid.UUID
}

func parseCardIDs(c storedCard) (cardIDs, error) {
	var ids cardIDs
	var err error
	if ids.card, err = uuidOf(c.ID); err != nil {
		return ids, fmt.Errorf("parse card uuid: %w", err)
	}
	if ids.set, err = uuidOf(c.SetID); err != nil {
		return ids, fmt.Errorf("parse set uuid: %w", err)
	}
	if ids.fact, err = uuidOf(c.FactID); err != nil {
		return ids, fmt.Errorf("parse fact uuid: %w", err)
	}
	return ids, nil
}

func uuidOf(id pgtype.UUID) (uuid.UUID, error) {
	return uuid.FromBytes(id.Bytes[:])
}

// revealOf is what a card hides from the feed until it is answered.
func revealOf(c storedCard, d cardDetails) contracts.CardReveal {
	r := contracts.CardReveal{
		Explanation: c.Explanation,
		SourceQuote: c.SourceQuote,
	}
	if c.SourceRef.Valid && c.SourceRef.String != "" {
		r.SourceRef.SetTo(c.SourceRef.String)
	}
	stored := c.AnswerText.String
	switch contracts.CardKind(c.Kind) {
	case contracts.CardKindChoice:
		r.Answer = choiceAnswer(d.options, stored)
	case contracts.CardKindBoolean:
		b, _ := strconv.ParseBool(stored)
		r.Answer = contracts.NewBoolCardAnswer(b)
	case contracts.CardKindTable:
		r.Answer = contracts.NewTableLayoutCardAnswer(d.table)
	default:
		// flip or input
		r.Answer = contracts.NewStringCardAnswer(stored)
	}
	return r
}

// fullCard is the whole card, as its author sees it.
func fullCard(c storedCard, d cardDetails) (contracts.Card, error) {
	card, err := questionCard(c)
	if err != nil {
		return card, err
	}
	reveal := revealOf(c, d)
	card.Answer.SetTo(reveal.Answer)
	card.Explanation.SetTo(reveal.Explanation)
	card.SourceQuote.SetTo(reveal.SourceQuote)
	card.SourceRef = reveal.SourceRef
	switch contracts.CardKind(c.Kind) {
	case contracts.CardKindChoice:
		card.Options = d.optionTexts()
	case contracts.CardKindTable:
		card.Table.SetTo(d.table)
	}
	return card, nil
}

// questionCard is the card without anything that gives the answer away, as
// a set member sees it in the list of the set's cards.
func questionCard(c storedCard) (contracts.Card, error) {
	ids, err := parseCardIDs(c)
	if err != nil {
		return contracts.Card{}, err
	}
	return contracts.Card{
		ID:       ids.card,
		SetId:    ids.set,
		FactId:   ids.fact,
		FactName: c.FactName,
		Kind:     contracts.CardKind(c.Kind),
		Question: c.Question,
	}, nil
}

// feedCardOf is the card as the feed asks it: the options of a choice card
// and the terms of a table card without their columns, both shuffled, since
// generated cards store the right options first and the terms column by
// column.
func feedCardOf(c storedCard, d cardDetails) (contracts.FeedCard, error) {
	ids, err := parseCardIDs(c)
	if err != nil {
		return contracts.FeedCard{}, err
	}
	card := contracts.FeedCard{
		ID:       ids.card,
		SetId:    ids.set,
		FactId:   ids.fact,
		FactName: c.FactName,
		Kind:     contracts.CardKind(c.Kind),
		Question: c.Question,
	}
	switch card.Kind {
	case contracts.CardKindChoice:
		card.Options = shuffled(d.optionTexts())
	case contracts.CardKindTable:
		items := make([]string, 0, len(d.table.Items))
		for _, item := range d.table.Items {
			items = append(items, item.Text)
		}
		card.Table.SetTo(contracts.FeedTable{Columns: d.table.Columns, Items: shuffled(items)})
	}
	return card, nil
}

func shuffled(items []string) []string {
	items = slices.Clone(items)
	rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return items
}
