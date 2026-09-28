package usecase

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type homescreenServiceImpl struct {
	querier     db.Querier
	userService UserService
}

// NewHomescreenService creates a new HomescreenService implementation backed by db.Querier.
func NewHomescreenService(querier db.Querier, userService UserService) HomescreenService {
	return &homescreenServiceImpl{
		querier:     querier,
		userService: userService,
	}
}

func (s *homescreenServiceImpl) GetToday(ctx context.Context, userID int64) (*contracts.TodayData, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	user, err := s.querier.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	activeDays, err := s.querier.CountUserActiveDays(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count active days: %w", err)
	}

	dueCount, err := s.querier.CountTotalDueCardsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count due cards: %w", err)
	}

	setRows, err := s.querier.GetUserSets(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user sets: %w", err)
	}

	sets := make([]contracts.CardSet, 0, len(setRows))
	for _, r := range setRows {
		setUUID, err := uuid.FromBytes(r.ID.Bytes[:])
		if err != nil {
			return nil, fmt.Errorf("parse set uuid: %w", err)
		}
		author := buildUserContract(r.AuthorID, r.AuthorFirstName, r.AuthorLastName, r.AuthorUsername)
		item := contracts.CardSet{
			ID:         setUUID,
			Title:      r.Title,
			CardsTotal: int(r.CardsTotal),
			CardsDue:   int(r.CardsDue),
			Author:     author,
		}
		if r.ShareCode != "" {
			item.ShareCode.SetTo(r.ShareCode)
		}
		sets = append(sets, item)
	}

	// Calculate estimated minutes (~15 seconds per card as per product concept)
	estimatedMinutes := int(math.Ceil(float64(dueCount) * 15.0 / 60.0))

	todayUser := buildUserContract(user.ID, user.FirstName, user.LastName, user.Username)

	return &contracts.TodayData{
		User:             todayUser,
		ActiveDays:       int(activeDays),
		DueCount:         int(dueCount),
		EstimatedMinutes: estimatedMinutes,
		Sets:             sets,
	}, nil
}

func (s *homescreenServiceImpl) GetFeedQuestions(ctx context.Context, userID int64) ([]contracts.Card, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	rows, err := s.querier.GetFeedCardsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get feed cards for user: %w", err)
	}

	cards := make([]contracts.Card, 0, len(rows))
	for _, r := range rows {
		c, err := s.buildFeedCard(ctx, r)
		if err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}

	return cards, nil
}

func (s *homescreenServiceImpl) SendResults(ctx context.Context, userID int64, results []contracts.AnswerResult) error {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return err
	}

	for _, res := range results {
		var pgCardID pgtype.UUID
		if err := pgCardID.Scan(res.CardId.String()); err != nil {
			return fmt.Errorf("%w: invalid card id %s", ErrValidation, res.CardId)
		}

		card, err := s.querier.GetCardByID(ctx, db.GetCardByIDParams{
			ID:     pgCardID,
			UserID: userID,
		})
		if err != nil {
			return fmt.Errorf("card %s not found: %w", res.CardId, ErrNotFound)
		}

		var answeredAt pgtype.Timestamptz
		_ = answeredAt.Scan(res.AnsweredAt)

		_, err = s.querier.RecordAnswerResult(ctx, db.RecordAnswerResultParams{
			UserID:     userID,
			CardID:     pgCardID,
			Correct:    res.Correct,
			AnsweredAt: answeredAt,
		})
		if err != nil {
			return fmt.Errorf("record answer result: %w", err)
		}

		_, err = s.querier.UpdateFactProgressOnAnswer(ctx, db.UpdateFactProgressOnAnswerParams{
			UserID:  userID,
			FactID:  card.FactID,
			Column3: res.Correct,
		})
		if err != nil {
			return fmt.Errorf("update fact progress on answer: %w", err)
		}
	}

	return nil
}

func (s *homescreenServiceImpl) buildFeedCard(ctx context.Context, r db.GetFeedCardsForUserRow) (contracts.Card, error) {
	cardUUID, err := uuid.FromBytes(r.ID.Bytes[:])
	if err != nil {
		return contracts.Card{}, fmt.Errorf("parse card uuid: %w", err)
	}
	setUUID, err := uuid.FromBytes(r.SetID.Bytes[:])
	if err != nil {
		return contracts.Card{}, fmt.Errorf("parse set uuid: %w", err)
	}

	cardKind := contracts.CardKind(r.Kind)
	card := contracts.Card{
		ID:          cardUUID,
		SetId:       setUUID,
		Kind:        cardKind,
		Question:    r.Question,
		Explanation: r.Explanation,
		SourceQuote: r.SourceQuote,
		Topic:       r.Topic,
	}

	if r.SourceRef.Valid && r.SourceRef.String != "" {
		card.SourceRef.SetTo(r.SourceRef.String)
	}

	ansStr := ""
	if r.AnswerText.Valid {
		ansStr = r.AnswerText.String
	}

	switch cardKind {
	case contracts.CardKindChoice:
		opts, err := s.querier.GetCardOptions(ctx, r.ID)
		if err != nil {
			return contracts.Card{}, fmt.Errorf("get card options: %w", err)
		}
		card.Options = make([]string, 0, len(opts))
		for _, o := range opts {
			card.Options = append(card.Options, o.Text)
		}
		card.Answer = contracts.CardAnswer{
			Type:   contracts.StringCardAnswer,
			String: ansStr,
		}
	case contracts.CardKindBoolean:
		b, _ := strconv.ParseBool(ansStr)
		card.Answer = contracts.CardAnswer{
			Type: contracts.BoolCardAnswer,
			Bool: b,
		}
	case contracts.CardKindTable:
		cols, err := s.querier.GetCardTableColumns(ctx, r.ID)
		if err != nil {
			return contracts.Card{}, fmt.Errorf("get card table columns: %w", err)
		}
		items, err := s.querier.GetCardTableItems(ctx, r.ID)
		if err != nil {
			return contracts.Card{}, fmt.Errorf("get card table items: %w", err)
		}

		tableLayout := contracts.TableLayout{
			Columns: make([]string, 0, len(cols)),
			Items:   make([]contracts.TableItem, 0, len(items)),
		}
		for _, col := range cols {
			tableLayout.Columns = append(tableLayout.Columns, col.Name)
		}
		for _, item := range items {
			tableLayout.Items = append(tableLayout.Items, contracts.TableItem{
				Text:   item.ItemText,
				Column: item.ColumnName,
			})
		}
		card.Table.SetTo(tableLayout)
		card.Answer = contracts.CardAnswer{
			Type:        contracts.TableLayoutCardAnswer,
			TableLayout: tableLayout,
		}
	default:
		card.Answer = contracts.CardAnswer{
			Type:   contracts.StringCardAnswer,
			String: ansStr,
		}
	}

	return card, nil
}
