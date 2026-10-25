package usecase

import (
	"context"
	"fmt"
	"math"

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

func (s *homescreenServiceImpl) GetToday(ctx context.Context, userID uuid.UUID) (*contracts.TodayData, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	user, err := s.querier.GetUserByID(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	dueCount, err := s.querier.CountTotalDueCardsForUser(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("count due cards: %w", err)
	}

	nextReviewAt, err := s.querier.GetNextReviewDateForUser(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("get next review date: %w", err)
	}

	setRows, err := s.querier.GetUserSets(ctx, pgUserID)
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
	setAvatar(&todayUser, user.AvatarID)

	var optNextReviewAt contracts.OptNilDateTime
	if nextReviewAt.Valid {
		optNextReviewAt.SetTo(nextReviewAt.Time)
	}

	return &contracts.TodayData{
		User:             todayUser,
		DueCount:         int(dueCount),
		EstimatedMinutes: estimatedMinutes,
		NextReviewAt:     optNextReviewAt,
		Sets:             sets,
	}, nil
}

// GetFeedQuestions asks the cards of the due facts, without their answers:
// those come back from AnswerQuestion.
func (s *homescreenServiceImpl) GetFeedQuestions(ctx context.Context, userID uuid.UUID, opts FeedOptions) ([]contracts.FeedCard, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	var rows []storedCard
	if opts.SetID != nil && opts.Early {
		setRows, err := s.querier.GetSetFeedCardsForUser(ctx, db.GetSetFeedCardsForUserParams{
			UserID: pgUserID,
			SetID:  pgtype.UUID{Bytes: *opts.SetID, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("get set feed cards for user: %w", err)
		}
		rows = make([]storedCard, len(setRows))
		for i, r := range setRows {
			rows[i] = storedCard(r)
		}
	} else {
		dueRows, err := s.querier.GetFeedCardsForUser(ctx, pgUserID)
		if err != nil {
			return nil, fmt.Errorf("get feed cards for user: %w", err)
		}
		if opts.SetID != nil {
			targetSetID := pgtype.UUID{Bytes: *opts.SetID, Valid: true}
			for _, r := range dueRows {
				if r.SetID == targetSetID {
					rows = append(rows, storedCard(r))
				}
			}
		} else {
			rows = make([]storedCard, len(dueRows))
			for i, r := range dueRows {
				rows[i] = storedCard(r)
			}
		}
	}

	var choiceIDs, tableIDs []pgtype.UUID
	for _, row := range rows {
		switch row.Kind {
		case "choice":
			choiceIDs = append(choiceIDs, row.ID)
		case "table":
			tableIDs = append(tableIDs, row.ID)
		}
	}
	details, err := loadCardDetails(ctx, s.querier, choiceIDs, tableIDs)
	if err != nil {
		return nil, err
	}

	cards := make([]contracts.FeedCard, 0, len(rows))
	for _, card := range rows {
		d, err := loadDetails(ctx, details, card)
		if err != nil {
			return nil, err
		}
		c, err := feedCardOf(card, d)
		if err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}

	return cards, nil
}
