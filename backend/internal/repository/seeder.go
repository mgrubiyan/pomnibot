package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// SaveGeneratedSet saves a generator.Result into the database as a new card set for the given user.
func SaveGeneratedSet(ctx context.Context, q db.Querier, userID int64, title string, genResult generator.Result) (*db.Set, int, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Новый конспект"
	}

	shareCode, err := generateUniqueShareCode(ctx, q)
	if err != nil {
		return nil, 0, fmt.Errorf("generate unique share code: %w", err)
	}

	newSet, err := q.CreateSet(ctx, db.CreateSetParams{
		Title:     title,
		AuthorID:  userID,
		ShareCode: shareCode,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("create user set: %w", err)
	}

	if _, err := q.JoinSet(ctx, db.JoinSetParams{
		UserID: userID,
		SetID:  newSet.ID,
	}); err != nil {
		return nil, 0, fmt.Errorf("enroll user in set: %w", err)
	}

	topicCache := make(map[string]pgtype.UUID)
	factsInserted := 0
	cardsInserted := 0

	for _, f := range genResult.Facts {
		var topicID pgtype.UUID
		if f.Topic != "" {
			if tid, ok := topicCache[f.Topic]; ok {
				topicID = tid
			} else {
				t, err := q.UpsertTopic(ctx, f.Topic)
				if err != nil {
					slog.ErrorContext(ctx, "failed to upsert topic", "topic", f.Topic, "error", err)
				} else {
					topicID = t.ID
					topicCache[f.Topic] = topicID
				}
			}
		}

		name := f.Name
		if strings.TrimSpace(name) == "" {
			name = f.Topic
		}
		if strings.TrimSpace(name) == "" {
			name = "Факт"
		}

		_, err := q.CreateFact(ctx, db.CreateFactParams{
			ID:      f.ID,
			SetID:   newSet.ID,
			TopicID: topicID,
			Name:    name,
		})
		if err == nil {
			factsInserted++
		} else {
			slog.WarnContext(ctx, "failed to create fact", "factID", f.ID, "error", err)
		}
	}

	for _, c := range genResult.Cards {
		cardUUID := uuid.New()
		var cardID pgtype.UUID
		_ = cardID.Scan(cardUUID.String())

		createdCard, err := q.CreateCard(ctx, db.CreateCardParams{
			ID:       cardID,
			FactID:   c.FactID,
			Kind:     string(c.Kind),
			Question: c.Question,
			AnswerText: pgtype.Text{
				String: c.Answer,
				Valid:  c.Answer != "",
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
				isCorrect := strings.TrimSpace(opt) == strings.TrimSpace(c.Answer)
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
	}

	if err := q.InitUserFactProgress(ctx, db.InitUserFactProgressParams{
		UserID: userID,
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

		// Avoid reserved default course code
		if code == "101101" {
			continue
		}

		_, err = q.GetSetByShareCode(ctx, db.GetSetByShareCodeParams{
			ShareCode: code,
			UserID:    0,
		})
		if err != nil {
			// Not found means it is available
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique share code after multiple attempts")
}
