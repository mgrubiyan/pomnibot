package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type setServiceImpl struct {
	manual      ManualWriter
	querier     db.Querier
	userService UserService
}

// NewSetService creates a new SetService implementation backed by db.Querier.
func NewSetService(querier db.Querier, userService UserService, manual ...ManualWriter) SetService {
	s := &setServiceImpl{
		querier:     querier,
		userService: userService,
	}
	if len(manual) > 0 {
		s.manual = manual[0]
	}
	return s
}

func (s *setServiceImpl) GetSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	row, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: pgUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	author := buildUserContract(row.AuthorID, row.AuthorFirstName, row.AuthorLastName, row.AuthorUsername)
	res, err := mapSetRowToContract(row.ID, row.Title, author, row.ShareCode, row.CardsTotal, row.CardsDue)
	if err != nil {
		return nil, err
	}

	if row.AuthorID == pgUserID {
		res.Warnings = row.Warnings
	}

	// The rating is optional: a set is shown without it when it cannot be read.
	rating, err := s.querier.GetUserSetRating(ctx, db.GetUserSetRatingParams{
		SetID:  pgUUID,
		UserID: pgUserID,
	})
	switch {
	case err == nil:
		res.UserRank.SetTo(int(rating.Rank))
		res.UserPercentile.SetTo(int(rating.Percentile))
	case !errors.Is(err, pgx.ErrNoRows):
		slog.WarnContext(ctx, "failed to get user set rating", "set_id", setID, "user_id", userID, "error", err)
	}

	return res, nil
}

func (s *setServiceImpl) DeleteSet(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("check set author: %w", err)
	}

	if isAuthor {
		rows, err := s.querier.DeleteSet(ctx, db.DeleteSetParams{
			ID:       pgUUID,
			AuthorID: pgUserID,
		})
		if err != nil {
			return fmt.Errorf("delete set: %w", err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	}

	// If not author, user leaves the set
	rows, err := s.querier.LeaveSet(ctx, db.LeaveSetParams{
		SetID:  pgUUID,
		UserID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("leave set: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *setServiceImpl) UpdateSetTitle(ctx context.Context, userID uuid.UUID, setID uuid.UUID, title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return fmt.Errorf("%w: set title cannot be empty", ErrValidation)
	}

	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return fmt.Errorf("check set author: %w", err)
	}

	if !isAuthor {
		exists, err := s.querier.SetExists(ctx, pgUUID)
		if err != nil {
			return fmt.Errorf("check set exists: %w", err)
		}
		if exists {
			return ErrForbidden
		}
		return ErrNotFound
	}

	rows, err := s.querier.UpdateSetTitle(ctx, db.UpdateSetTitleParams{
		ID:       pgUUID,
		AuthorID: pgUserID,
		Title:    trimmed,
	})
	if err != nil {
		return fmt.Errorf("update set title: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *setServiceImpl) GetCardsBySetID(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.Card, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	// Verify set exists and user has membership
	set, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: pgUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	if set.AuthorID != pgUserID {
		return nil, ErrForbidden
	}

	rows, err := s.querier.GetCardsBySetID(ctx, db.GetCardsBySetIDParams{
		SetID:  pgUUID,
		UserID: pgUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("get cards by set id: %w", err)
	}

	var choiceIDs, tableIDs, imageIDs []pgtype.UUID
	for _, row := range rows {
		imageIDs = append(imageIDs, row.ID)
		switch row.Kind {
		case "choice":
			choiceIDs = append(choiceIDs, row.ID)
		case "table":
			tableIDs = append(tableIDs, row.ID)
		}
	}
	details, err := loadCardDetails(ctx, s.querier, choiceIDs, tableIDs, imageIDs)
	if err != nil {
		return nil, err
	}

	cards := make([]contracts.Card, 0, len(rows))
	for _, r := range rows {
		card := setCardOf(r)
		d, err := loadDetails(ctx, details, card)
		if err != nil {
			return nil, err
		}
		c, err := fullCard(card, d)
		if err != nil {
			return nil, err
		}
		c.Step.SetTo(int(r.Step))
		cards = append(cards, c)
	}

	return cards, nil
}

func (s *setServiceImpl) GetSetShareCode(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.CardSet, error) {
	return s.GetSet(ctx, userID, setID)
}

func (s *setServiceImpl) GetSetPlan(ctx context.Context, userID uuid.UUID, setID uuid.UUID) ([]contracts.SetPlanItem, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	// Verify set exists and user has membership
	if _, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: pgUserID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	rows, err := s.querier.GetSetPlan(ctx, db.GetSetPlanParams{
		SetID:  pgUUID,
		UserID: pgUserID,
	})
	if err != nil {
		return nil, fmt.Errorf("get set plan: %w", err)
	}

	plan := make([]contracts.SetPlanItem, 0, len(rows))
	for _, r := range rows {
		t := r.Date.Time
		plan = append(plan, contracts.SetPlanItem{
			FactName: r.FactName,
			Date:     t,
		})
	}
	return plan, nil
}

func (s *setServiceImpl) GetSetLeaderboard(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (*contracts.SetLeaderboardResponse, error) {
	pgUUID, err := s.authorSetID(ctx, userID, setID)
	if err != nil {
		return nil, err
	}

	rows, err := s.querier.GetSetLeaderboard(ctx, pgUUID)
	if err != nil {
		return nil, fmt.Errorf("get set leaderboard: %w", err)
	}

	items := make([]contracts.LeaderboardEntry, 0, len(rows))
	for _, r := range rows {
		user := memberContract(userID, r.UserID, r.FirstName, r.LastName, r.Username, r.AvatarID, r.DataConsent)
		items = append(items, contracts.LeaderboardEntry{
			User:       user,
			Rank:       int(r.Rank),
			Percentile: int(r.Percentile),
		})
	}

	return &contracts.SetLeaderboardResponse{
		SetId: setID,
		Items: items,
	}, nil
}

// authorSetID checks that the set exists for the caller and that the caller
// wrote it.
func (s *setServiceImpl) authorSetID(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (pgtype.UUID, error) {
	pgUUID := pgtype.UUID{Bytes: setID, Valid: true}
	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	if _, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: pgUserID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, ErrNotFound
		}
		return pgtype.UUID{}, fmt.Errorf("get set by id: %w", err)
	}

	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgUUID,
		AuthorID: pgUserID,
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("check set author: %w", err)
	}
	if !isAuthor {
		return pgtype.UUID{}, ErrForbidden
	}
	return pgUUID, nil
}

func (s *setServiceImpl) JoinSetByShareCode(ctx context.Context, userID uuid.UUID, code string) (*contracts.CardSet, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("%w: share code is required", ErrValidation)
	}

	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	row, err := s.querier.GetSetByShareCode(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by share code: %w", err)
	}

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	if _, err := s.querier.JoinSet(ctx, db.JoinSetParams{
		UserID: pgUserID,
		SetID:  row.ID,
	}); err != nil {
		return nil, fmt.Errorf("join set: %w", err)
	}

	if err := s.querier.InitUserFactProgress(ctx, db.InitUserFactProgressParams{
		UserID: pgUserID,
		SetID:  row.ID,
	}); err != nil {
		return nil, fmt.Errorf("init user fact progress: %w", err)
	}

	author := buildUserContract(row.AuthorID, row.AuthorFirstName, row.AuthorLastName, row.AuthorUsername)
	return mapSetRowToContract(row.ID, row.Title, author, row.ShareCode, row.CardsTotal, 0)
}

func (s *setServiceImpl) SaveGeneratedSet(ctx context.Context, userID uuid.UUID, title string, genResult generator.Result, warnings []string) (*contracts.CardSet, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	prepared, err := prepareGeneratedSet(title, genResult, warnings)
	if err != nil {
		return nil, fmt.Errorf("prepare generated set: %w", err)
	}
	var set *db.Set
	var cardsCount int
	save := func(q db.Querier) error {
		var err error
		set, cardsCount, err = repository.SaveGeneratedSet(ctx, q, userID, title, prepared)
		return err
	}
	if tx, ok := s.manual.(Transactor); ok {
		err = tx.InTx(ctx, save)
	} else {
		err = save(s.querier)
	}
	if err != nil {
		return nil, fmt.Errorf("save generated set for user: %w", err)
	}

	setUUID, err := uuid.FromBytes(set.ID.Bytes[:])
	if err != nil {
		return nil, fmt.Errorf("parse set uuid: %w", err)
	}

	res := &contracts.CardSet{
		ID:         setUUID,
		Title:      set.Title,
		Warnings:   set.Warnings,
		CardsTotal: cardsCount,
		CardsDue:   cardsCount,
	}
	if set.ShareCode != "" {
		res.ShareCode.SetTo(set.ShareCode)
	}
	return res, nil
}

func mapSetRowToContract(id pgtype.UUID, title string, author contracts.User, shareCode string, cardsTotal, cardsDue int32) (*contracts.CardSet, error) {
	setUUID, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return nil, fmt.Errorf("parse set uuid: %w", err)
	}

	res := &contracts.CardSet{
		ID:         setUUID,
		Title:      title,
		CardsTotal: int(cardsTotal),
		CardsDue:   int(cardsDue),
		Author:     author,
	}
	if shareCode != "" {
		res.ShareCode.SetTo(shareCode)
	}
	return res, nil
}

// memberContract shows the author a member of their set by name and photo:
// joining the set by its code is the member's consent to that. The one
// exception is the shared account every unsigned request gets (uuid.Nil):
// it is anyone at all, so it sees only members who agreed on the old consent
// screen, and itself. A member hidden from it gets a new random ID on every
// call: their own ID would let it find their name wherever else it shows,
// such as on a set they wrote.
func memberContract(viewer uuid.UUID, id pgtype.UUID, firstName string, lastName, username pgtype.Text, avatar pgtype.UUID, consent bool) contracts.User {
	if viewer != uuid.Nil || consent || (id.Valid && id.Bytes == uuid.Nil) {
		u := buildUserContract(id, firstName, lastName, username)
		setAvatar(&u, avatar)
		return u
	}
	return contracts.User{ID: uuid.New(), Anonymous: contracts.NewOptBool(true)}
}

func buildUserContract(id pgtype.UUID, firstName string, lastName, username pgtype.Text) contracts.User {
	effectiveFirstName := strings.TrimSpace(firstName)
	var uid uuid.UUID
	if id.Valid {
		uid = id.Bytes
	}
	if effectiveFirstName == "" {
		if username.Valid && strings.TrimSpace(username.String) != "" {
			effectiveFirstName = strings.TrimSpace(username.String)
		} else {
			effectiveFirstName = fmt.Sprintf("User %s", uid)
		}
	}

	u := contracts.User{
		ID:        uid,
		FirstName: effectiveFirstName,
	}
	if lastName.Valid && strings.TrimSpace(lastName.String) != "" {
		u.LastName.SetTo(strings.TrimSpace(lastName.String))
	}
	if username.Valid && strings.TrimSpace(username.String) != "" {
		u.Username.SetTo(strings.TrimSpace(username.String))
	}
	return u
}
