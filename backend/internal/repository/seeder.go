package repository

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
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
func SeedIfEmpty(ctx context.Context, dbtx db.DBTX, queries db.Querier) error {
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

// SeedFromData populates the database with cards and sets from raw JSON bytes for the default course.
func SeedFromData(ctx context.Context, queries db.Querier, dataBytes []byte) error {
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

	factsInserted, cardsInserted, err := populateCards(ctx, queries, setID, data.Cards, false)
	if err != nil {
		return fmt.Errorf("populate default cards: %w", err)
	}

	slog.InfoContext(ctx, "mock data seeding completed successfully",
		"factsInserted", factsInserted,
		"cardsInserted", cardsInserted,
	)
	return nil
}

// GenerateSetForUser creates a new study set for a user based on template cards.
// Each call generates a collision-safe 6-digit share code and unique UUIDs for facts and cards.
func GenerateSetForUser(ctx context.Context, q db.Querier, userID int64, title string) (*db.Set, int, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Новый конспект"
	}

	var data mockData
	if err := json.Unmarshal(defaultSeedData, &data); err != nil {
		return nil, 0, fmt.Errorf("parse default seed data: %w", err)
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

	factsInserted, cardsInserted, err := populateCards(ctx, q, newSet.ID, data.Cards, true)
	if err != nil {
		return nil, 0, fmt.Errorf("populate user cards: %w", err)
	}

	// Initialize user fact progress so cards appear in feed & today
	if err := q.InitUserFactProgress(ctx, db.InitUserFactProgressParams{
		UserID: userID,
		SetID:  newSet.ID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to initialize user fact progress", "error", err, "userID", userID)
	}

	slog.InfoContext(ctx, "generated mock set for user",
		"userID", userID,
		"setID", newSet.ID,
		"title", newSet.Title,
		"shareCode", newSet.ShareCode,
		"factsCount", factsInserted,
		"cardsCount", cardsInserted,
	)

	return &newSet, cardsInserted, nil
}

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

func populateCards(ctx context.Context, q db.Querier, setID pgtype.UUID, cards []rawCard, randomizeFactIDs bool) (int, int, error) {
	topicCache := make(map[string]pgtype.UUID)
	factIDMap := make(map[string]string)
	factsInserted := 0
	cardsInserted := 0

	for _, rc := range cards {
		topicID, ok := topicCache[rc.Topic]
		if !ok && rc.Topic != "" {
			t, err := q.UpsertTopic(ctx, rc.Topic)
			if err != nil {
				slog.ErrorContext(ctx, "failed to upsert topic", "topic", rc.Topic, "error", err)
			} else {
				topicID = t.ID
				topicCache[rc.Topic] = topicID
			}
		}

		factID := rc.FactID
		if mapped, exists := factIDMap[rc.FactID]; exists {
			factID = mapped
		} else {
			if randomizeFactIDs {
				factID = uuid.New().String()
			}
			factIDMap[rc.FactID] = factID

			_, err := q.CreateFact(ctx, db.CreateFactParams{
				ID:      factID,
				SetID:   setID,
				TopicID: topicID,
				Name:    rc.FactName,
			})
			if err == nil {
				factsInserted++
			} else {
				slog.WarnContext(ctx, "failed to create fact", "factID", factID, "error", err)
			}
		}

		answerStr := formatAnswerString(rc.Answer)
		cardUUID := uuid.New()
		var cardID pgtype.UUID
		_ = cardID.Scan(cardUUID.String())

		createdCard, err := q.CreateCard(ctx, db.CreateCardParams{
			ID:       cardID,
			FactID:   factID,
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

		if rc.Kind == "choice" && len(rc.Options) > 0 {
			for idx, opt := range rc.Options {
				isCorrect := strings.TrimSpace(opt) == strings.TrimSpace(answerStr)
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

		if rc.Kind == "table" && rc.Table != nil {
			for idx, col := range rc.Table.Columns {
				_, _ = q.CreateCardTableColumn(ctx, db.CreateCardTableColumnParams{
					CardID:   createdCard.ID,
					Position: int32(idx),
					Name:     col,
				})
			}
			for _, item := range rc.Table.Items {
				_, _ = q.CreateCardTableItem(ctx, db.CreateCardTableItemParams{
					CardID:     createdCard.ID,
					ItemText:   item.Text,
					ColumnName: item.Column,
				})
			}
		}
	}

	return factsInserted, cardsInserted, nil
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
