package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// GeneratedSet contains storage models prepared by the usecase and the private report.
type GeneratedSet struct {
	Warnings []string
	Facts    []cards.Fact
	Cards    []cards.Card
	Report   generator.Result
}

// SaveGeneratedSet persists the prepared models and their assigned fact identifiers.
func SaveGeneratedSet(ctx context.Context, q db.Querier, userID uuid.UUID, title string, genResult GeneratedSet) (*db.Set, int, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Новый конспект"
	}

	shareCode, err := generateUniqueShareCode(ctx, q)
	if err != nil {
		return nil, 0, fmt.Errorf("generate unique share code: %w", err)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	newSet, err := q.CreateSet(ctx, db.CreateSetParams{
		Title:     title,
		AuthorID:  pgUserID,
		ShareCode: shareCode,
		Warnings:  append([]string{}, genResult.Warnings...),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("create user set: %w", err)
	}

	if genResult.Report.Artifact != nil {
		data, marshalErr := json.Marshal(genResult.Report)
		if marshalErr != nil {
			return nil, 0, fmt.Errorf("encode private processing report: %w", marshalErr)
		}
		affected, saveErr := q.SaveProcessingReport(ctx, db.SaveProcessingReportParams{ID: newSet.ID, AuthorID: pgUserID, Version: genResult.Report.Artifact.Version, Report: data})
		if saveErr != nil {
			return nil, 0, fmt.Errorf("save private processing report: %w", saveErr)
		}
		if affected != 1 {
			return nil, 0, errors.New("processing report owner mismatch")
		}
	}

	if _, err := q.JoinSet(ctx, db.JoinSetParams{
		UserID: pgUserID,
		SetID:  newSet.ID,
	}); err != nil {
		return nil, 0, fmt.Errorf("enroll user in set: %w", err)
	}

	factsInserted := 0
	cardsInserted := 0

	for _, f := range genResult.Facts {
		name := f.Name
		if strings.TrimSpace(name) == "" {
			name = f.Topic
		}
		if strings.TrimSpace(name) == "" {
			name = "Факт"
		}

		factID, err := uuid.Parse(f.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("parse generated fact ID: %w", err)
		}
		_, err = q.CreateFact(ctx, db.CreateFactParams{
			ID:    pgtype.UUID{Bytes: factID, Valid: true},
			SetID: newSet.ID,
			Name:  name,
		})
		if err == nil {
			factsInserted++
		} else {
			slog.WarnContext(ctx, "failed to create fact", "factID", f.ID, "error", err)
		}
	}

	// The feed selects each fact's step by card UUID order.
	for _, c := range genResult.Cards {
		cardUUID, err := uuid.NewV7()
		if err != nil {
			return nil, 0, fmt.Errorf("generate card UUIDv7: %w", err)
		}
		cardID := pgtype.UUID{Bytes: cardUUID, Valid: true}

		answer := c.Answer
		if c.Kind == cards.KindChoice && len(c.RightAnswers) > 0 {
			data, _ := json.Marshal(c.RightAnswers)
			answer = string(data)
		}
		factID, err := uuid.Parse(c.FactID)
		if err != nil {
			return nil, 0, fmt.Errorf("parse generated card fact ID: %w", err)
		}
		createdCard, err := q.CreateCard(ctx, db.CreateCardParams{
			ID:       cardID,
			FactID:   pgtype.UUID{Bytes: factID, Valid: true},
			Kind:     db.CardKind(c.Kind),
			Question: c.Question,
			AnswerText: pgtype.Text{
				String: answer,
				Valid:  answer != "",
			},
			Explanation: c.Explanation,
			SourceQuote: c.SourceQuote,
			SourceRef: pgtype.Text{
				String: c.SourceRef,
				Valid:  c.SourceRef != "",
			},
		})
		if err != nil {
			slog.WarnContext(ctx, "skipping card creation error", "question", c.Question, "error", err)
			continue
		}
		cardsInserted++

		if c.Kind == cards.KindChoice && len(c.Options) > 0 {
			for idx, opt := range c.Options {
				isCorrect := slices.Contains(c.RightAnswers, opt) || (len(c.RightAnswers) == 0 && strings.TrimSpace(opt) == strings.TrimSpace(c.Answer))
				_, err := q.CreateCardOption(ctx, db.CreateCardOptionParams{
					CardID:    createdCard.ID,
					Position:  int32(idx),
					Text:      opt,
					IsCorrect: isCorrect,
				})
				if err != nil {
					slog.ErrorContext(ctx, "failed to create card option", "error", err)
				}
			}
		}

		if c.Kind == cards.KindTable && c.Table != nil {
			for idx, col := range c.Table.Columns {
				if _, err := q.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
					CardID:   createdCard.ID,
					Position: int32(idx),
					Name:     col,
				}); err != nil {
					slog.ErrorContext(ctx, "failed to create card table column", "error", err)
				}
			}
			for idx, item := range c.Table.Items {
				if _, err := q.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
					CardID:     createdCard.ID,
					ItemText:   item.Text,
					ColumnName: item.Column,
					Position:   int32(idx), // retain source order; the frontend shuffles before display
				}); err != nil {
					slog.ErrorContext(ctx, "failed to create card table item", "error", err)
				}
			}
		}
	}

	if err := q.InitUserFactProgress(ctx, db.InitUserFactProgressParams{
		UserID: pgUserID,
		SetID:  newSet.ID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to initialize user fact progress", "error", err, "userID", userID)
	}

	slog.InfoContext(ctx, "saved generated set for user",
		"userID", userID,
		"setID", newSet.ID,
		"title", newSet.Title,
		"shareCode", newSet.ShareCode,
		"factsCount", factsInserted,
		"cardsCount", cardsInserted,
	)

	return &newSet, cardsInserted, nil
}

func generateUniqueShareCode(ctx context.Context, q db.Querier) (string, error) {
	for attempt := 0; attempt < 20; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return "", fmt.Errorf("generate random share code: %w", err)
		}
		code := fmt.Sprintf("%06d", 100000+n.Int64())

		_, err = q.GetSetByShareCode(ctx, code)
		if err != nil {
			// Not found means it is available
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique share code after multiple attempts")
}
