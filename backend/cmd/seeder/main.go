// Package main provides a CLI tool to seed the database with mock data from res.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

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

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	filePath := flag.String("file", "res.json", "Path to res.json file")
	flag.Parse()

	if *dbURL == "" {
		slog.Error("DATABASE_URL is required (pass -db flag or set DATABASE_URL environment variable)")
		os.Exit(1)
	}

	dataBytes, err := os.ReadFile(*filePath)
	if err != nil {
		slog.Error("failed to read mock data file", "path", *filePath, "error", err)
		os.Exit(1)
	}

	var data mockData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		slog.Error("failed to parse mock data JSON", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	queries := db.New(conn)

	// 1. Create or ensure default admin/author user
	const authorID int64 = 100001
	author, err := queries.UpsertUser(ctx, db.UpsertUserParams{
		ID:   authorID,
		Name: "Pomnibot Content Admin",
	})
	if err != nil {
		slog.Error("failed to create default user", "error", err)
		os.Exit(1)
	}
	slog.Info("author user ready", "userID", author.ID, "name", author.Name)

	// 2. Create or find default card set
	const defaultShareCode = "101101"
	var setID pgtype.UUID
	existingSet, err := queries.GetSetByShareCode(ctx, db.GetSetByShareCodeParams{
		ShareCode: defaultShareCode,
		UserID:    authorID,
	})
	if err == nil {
		setID = existingSet.ID
		slog.Info("using existing card set", "setID", setID, "title", existingSet.Title)
	} else {
		newSet, err := queries.CreateSet(ctx, db.CreateSetParams{
			Title:     "Введение в компиляторы и препроцессор C++",
			AuthorID:  authorID,
			ShareCode: defaultShareCode,
		})
		if err != nil {
			slog.Error("failed to create default card set", "error", err)
			os.Exit(1)
		}
		setID = newSet.ID
		slog.Info("created card set", "setID", setID, "shareCode", newSet.ShareCode)
	}

	// Also enroll author in user_sets
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
				slog.Error("failed to upsert topic", "topic", rc.Topic, "error", err)
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
			slog.Warn("skipping duplicate or failed card", "question", rc.Question, "error", err)
			continue
		}
		cardsInserted++

		// 6. If choice: insert card options
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
					slog.Error("failed to create card option", "error", err)
				}
			}
		}

		// 7. If table: insert table columns and items
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

	slog.Info("seeding complete",
		"topics", len(topicCache),
		"factsInserted", factsInserted,
		"cardsInserted", cardsInserted,
	)
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
