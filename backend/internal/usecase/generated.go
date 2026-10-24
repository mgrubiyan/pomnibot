package usecase

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
)

// prepareGeneratedSet assigns UUIDv7s sequentially so database reads retain fact order.
func prepareGeneratedSet(title string, result generator.Result, warnings []string) (repository.GeneratedSet, error) {
	out := repository.GeneratedSet{Report: result, Warnings: slices.Clone(warnings)}
	for _, fact := range result.Facts {
		id, err := uuid.NewV7()
		if err != nil {
			return repository.GeneratedSet{}, fmt.Errorf("generate fact UUIDv7: %w", err)
		}
		out.Facts = append(out.Facts, cards.Fact{ID: id.String(), Name: fact.Name, Topic: fact.Name})
		pages := make([]string, len(fact.Pages))
		for i, page := range fact.Pages {
			pages[i] = strconv.Itoa(page)
		}
		ref := title + ", стр. " + strings.Join(pages, ", ")
		for _, raw := range fact.Cards {
			if raw == nil {
				return repository.GeneratedSet{}, fmt.Errorf("nil generated card")
			}
			base := raw.Base()
			c := cards.Card{FactID: id.String(), Question: base.Question, Explanation: base.Source, SourceQuote: base.SourceQuote, SourceRef: ref}
			switch v := raw.(type) {
			case generator.FlipCard:
				c.Kind = cards.KindFlip
				c.Answer = v.Answer
			case generator.BooleanCard:
				c.Kind = cards.KindBoolean
				c.Answer = strconv.FormatBool(v.Answer)
			case generator.InputCard:
				c.Kind = cards.KindInput
				c.Answer = v.Answer
			case generator.ChoiceCard:
				c.Kind = cards.KindChoice
				c.RightAnswers = slices.Clone(v.RightAnswers)
				c.Options = append(slices.Clone(v.RightAnswers), v.WrongAnswers...)
				answer, _ := json.Marshal(v.RightAnswers)
				c.Answer = string(answer)
			case generator.TableCard:
				c.Kind = cards.KindTable
				c.Table = &cards.Table{}
				for _, col := range v.Columns {
					name := strings.TrimSpace(col.Name)
					c.Table.Columns = append(c.Table.Columns, name)
					for _, row := range col.Rows {
						c.Table.Items = append(c.Table.Items, cards.TableItem{Text: strings.TrimSpace(row), Column: name})
					}
				}
			default:
				return repository.GeneratedSet{}, fmt.Errorf("unsupported generated card %T", raw)
			}
			out.Cards = append(out.Cards, c)
		}
	}
	return out, nil
}
