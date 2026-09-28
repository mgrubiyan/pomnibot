package repository

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

//go:embed seeds/res.json
var defaultSeedData []byte

type rawCard struct {
	FactID      string          `json:"factId"`
	FactName    string          `json:"factName"`
	Kind        string          `json:"kind"`
	Question    string          `json:"question"`
	Options     []string        `json:"options,omitempty"`
	Table       *rawTableLayout `json:"table,omitempty"`
	Answer      any             `json:"answer"`
	Explanation string          `json:"explanation"`
	SourceQuote string          `json:"sourceQuote"`
	SourceRef   string          `json:"sourceRef"`
	Topic       string          `json:"topic"`
}

type rawTableLayout struct {
	Columns []string       `json:"columns"`
	Items   []rawTableItem `json:"items"`
}

type rawTableItem struct {
	Text   string `json:"text"`
	Column string `json:"column"`
}

type mockData struct {
	Cards []rawCard `json:"cards"`
}

// SeedIfEmpty checks whether the sets table contains any card sets.
// If the database is empty, it populates the default card set and mock cards from seeds/res.json.
func SeedIfEmpty(ctx context.Context, dbtx db.DBTX, queries *db.Queries) error {
	var count int64
	row := dbtx.QueryRow(ctx, "SELECT COUNT(*) FROM sets")
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("check existing sets count: %w", err)
	}

	if count > 0 {
		slog.InfoContext(ctx, "database already contains card sets, skipping auto-seed", "existingSets", count)
		return nil
	}

	slog.InfoContext(ctx, "database is empty, running auto-seed with default course...")
	return SeedFromData(ctx, queries, defaultSeedData)
}

// SeedFromData populates the database with cards and sets from raw JSON bytes.
func SeedFromData(ctx context.Context, queries *db.Queries, dataBytes []byte) error {
	var data mockData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return fmt.Errorf("parse mock data json: %w", err)
	}

	// 1. Create or ensure default admin/author user
	const authorID int64 = 100001
	adminLastName := "Admin"
	author, err := queries.UpsertUser(ctx, db.UpsertUserParams{
		ID:        authorID,
		FirstName: "Pomnibot Content",
		LastName:  pgtype.Text{String: adminLastName, Valid: true},
		Username:  pgtype.Text{Valid: false},
		IsBot:     false,
	})
	if err != nil {
		return fmt.Errorf("upsert default author: %w", err)
	}
	slog.InfoContext(ctx, "author user ready", "userID", author.ID, "firstName", author.FirstName)

	// 2. Create default card set
	const defaultShareCode = "101101"
	var setID pgtype.UUID
	existingSet, err := queries.GetSetByShareCode(ctx, db.GetSetByShareCodeParams{
		ShareCode: defaultShareCode,
		UserID:    authorID,
	})
	if err == nil {
		setID = existingSet.ID
	} else {
		newSet, err := queries.CreateSet(ctx, db.CreateSetParams{
			Title:     "Введение в компиляторы и препроцессор C++",
			AuthorID:  authorID,
			ShareCode: defaultShareCode,
		})
		if err != nil {
			return fmt.Errorf("create default set: %w", err)
		}
		setID = newSet.ID
	}

	// Enroll author in user_sets
	_, _ = queries.JoinSet(ctx, db.JoinSetParams{
		UserID: authorID,
		SetID:  setID,
	})

	topicCache := make(map[string]pgtype.UUID)
	factsInserted := 0
	cardsInserted := 0

	for _, rc := range data.Cards {
		// 3. Upsert Topic
		topicID, ok := topicCache[rc.Topic]
		if !ok && rc.Topic != "" {
			t, err := queries.UpsertTopic(ctx, rc.Topic)
			if err != nil {
				slog.ErrorContext(ctx, "failed to upsert topic", "topic", rc.Topic, "error", err)
				continue
			}
			topicID = t.ID
			topicCache[rc.Topic] = topicID
		}

		// 4. Create Fact if not present
		_, err := queries.CreateFact(ctx, db.CreateFactParams{
			ID:      rc.FactID,
			SetID:   setID,
			TopicID: topicID,
			Name:    rc.FactName,
		})
		if err == nil {
			factsInserted++
		}

		// 5. Create Card
		answerStr := formatAnswerString(rc.Answer)
		cardUUID := uuid.New()
		var cardID pgtype.UUID
		_ = cardID.Scan(cardUUID.String())

		createdCard, err := queries.CreateCard(ctx, db.CreateCardParams{
			ID:       cardID,
			FactID:   rc.FactID,
			Kind:     rc.Kind,
			Question: rc.Question,
			AnswerText: pgtype.Text{
				String: answerStr,
				Valid:  answerStr != "",
			},
			Explanation: rc.Explanation,
			SourceQuote: rc.SourceQuote,
			SourceRef: pgtype.Text{
				String: rc.SourceRef,
				Valid:  rc.SourceRef != "",
			},
		})
		if err != nil {
			slog.WarnContext(ctx, "skipping duplicate or failed card", "question", rc.Question, "error", err)
			continue
		}
		cardsInserted++

		// 6. Choice options
		if rc.Kind == "choice" && len(rc.Options) > 0 {
			for idx, opt := range rc.Options {
				isCorrect := strings.TrimSpace(opt) == strings.TrimSpace(answerStr)
				_, err := queries.CreateCardOption(ctx, db.CreateCardOptionParams{
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

		// 7. Table columns and items
		if rc.Kind == "table" && rc.Table != nil {
			for idx, col := range rc.Table.Columns {
				_, _ = queries.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
					CardID:   createdCard.ID,
					Position: int32(idx),
					Name:     col,
				})
			}
			for _, item := range rc.Table.Items {
				_, _ = queries.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
					CardID:     createdCard.ID,
					ItemText:   item.Text,
					ColumnName: item.Column,
				})
			}
		}
	}

	slog.InfoContext(ctx, "mock data seeding completed successfully",
		"topics", len(topicCache),
		"factsInserted", factsInserted,
		"cardsInserted", cardsInserted,
	)
	return nil
}

func formatAnswerString(ans any) string {
	if ans == nil {
		return ""
	}
	switch v := ans.(type) {
	case string:
		return v
	case bool:
		return fmt.Sprintf("%t", v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
