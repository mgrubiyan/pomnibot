package usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	querier     db.Querier
	userService UserService
}

// NewSetService creates a new SetService implementation backed by db.Querier.
func NewSetService(querier db.Querier, userService UserService) SetService {
	return &setServiceImpl{
		querier:     querier,
		userService: userService,
	}
}

func (s *setServiceImpl) GetSet(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	row, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	author := buildUserContract(row.AuthorID, row.AuthorFirstName, row.AuthorLastName, row.AuthorUsername)
	return mapSetRowToContract(row.ID, row.Title, author, row.ShareCode, row.CardsTotal, row.CardsDue)
}

func (s *setServiceImpl) DeleteSet(ctx context.Context, userID int64, setID uuid.UUID) error {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	isAuthor, err := s.querier.IsSetAuthor(ctx, db.IsSetAuthorParams{
		ID:       pgUUID,
		AuthorID: userID,
	})
	if err != nil {
		return fmt.Errorf("check set author: %w", err)
	}

	if isAuthor {
		rows, err := s.querier.DeleteSet(ctx, db.DeleteSetParams{
			ID:       pgUUID,
			AuthorID: userID,
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
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("leave set: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *setServiceImpl) GetCardsBySetID(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.Card, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	// Verify set exists and user has membership
	if _, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	rows, err := s.querier.GetCardsBySetID(ctx, db.GetCardsBySetIDParams{
		SetID:  pgUUID,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("get cards by set id: %w", err)
	}

	cards := make([]contracts.Card, 0, len(rows))
	for _, r := range rows {
		c, err := s.buildCard(ctx, r.ID, r.SetID, r.Kind, r.Question, r.AnswerText, r.Explanation, r.SourceQuote, r.SourceRef, r.Topic)
		if err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}

	return cards, nil
}

func (s *setServiceImpl) GetSetShareCode(ctx context.Context, userID int64, setID uuid.UUID) (*contracts.CardSet, error) {
	return s.GetSet(ctx, userID, setID)
}

func (s *setServiceImpl) GetSetPlan(ctx context.Context, userID int64, setID uuid.UUID) ([]contracts.SetPlanItem, error) {
	var pgUUID pgtype.UUID
	if err := pgUUID.Scan(setID.String()); err != nil {
		return nil, fmt.Errorf("%w: invalid set id", ErrValidation)
	}

	// Verify set exists and user has membership
	if _, err := s.querier.GetSetByID(ctx, db.GetSetByIDParams{
		ID:     pgUUID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by id: %w", err)
	}

	rows, err := s.querier.GetSetPlan(ctx, db.GetSetPlanParams{
		SetID:  pgUUID,
		UserID: userID,
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

func (s *setServiceImpl) JoinSetByShareCode(ctx context.Context, userID int64, code string) (*contracts.CardSet, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("%w: share code is required", ErrValidation)
	}

	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	row, err := s.querier.GetSetByShareCode(ctx, db.GetSetByShareCodeParams{
		ShareCode: code,
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get set by share code: %w", err)
	}

	if _, err := s.querier.JoinSet(ctx, db.JoinSetParams{
		UserID: userID,
		SetID:  row.ID,
	}); err != nil {
		return nil, fmt.Errorf("join set: %w", err)
	}

	if err := s.querier.InitUserFactProgress(ctx, db.InitUserFactProgressParams{
		UserID: userID,
		SetID:  row.ID,
	}); err != nil {
		return nil, fmt.Errorf("init user fact progress: %w", err)
	}

	author := buildUserContract(row.AuthorID, row.AuthorFirstName, row.AuthorLastName, row.AuthorUsername)
	return mapSetRowToContract(row.ID, row.Title, author, row.ShareCode, row.CardsTotal, row.CardsDue)
}

func (s *setServiceImpl) GenerateMockSet(ctx context.Context, userID int64, title string) (*contracts.CardSet, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	set, cardsCount, err := repository.GenerateSetForUser(ctx, s.querier, userID, title)
	if err != nil {
		return nil, fmt.Errorf("generate set for user: %w", err)
	}

	setUUID, err := uuid.FromBytes(set.ID.Bytes[:])
	if err != nil {
		return nil, fmt.Errorf("parse set uuid: %w", err)
	}

	res := &contracts.CardSet{
		ID:         setUUID,
		Title:      set.Title,
		CardsTotal: cardsCount,
		CardsDue:   cardsCount,
	}
	if set.ShareCode != "" {
		res.ShareCode.SetTo(set.ShareCode)
	}
	return res, nil
}

func (s *setServiceImpl) SaveGeneratedSet(ctx context.Context, userID int64, title string, genResult generator.Result) (*contracts.CardSet, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}

	set, cardsCount, err := repository.SaveGeneratedSet(ctx, s.querier, userID, title, genResult)
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
		CardsTotal: cardsCount,
		CardsDue:   cardsCount,
	}
	if set.ShareCode != "" {
		res.ShareCode.SetTo(set.ShareCode)
	}
	return res, nil
}

func (s *setServiceImpl) buildCard(
	ctx context.Context,
	id pgtype.UUID,
	setID pgtype.UUID,
	kind string,
	question string,
	answerText pgtype.Text,
	explanation string,
	sourceQuote string,
	sourceRef pgtype.Text,
	topic string,
) (contracts.Card, error) {
	cardUUID, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return contracts.Card{}, fmt.Errorf("parse card uuid: %w", err)
	}
	setUUID, err := uuid.FromBytes(setID.Bytes[:])
	if err != nil {
		return contracts.Card{}, fmt.Errorf("parse set uuid: %w", err)
	}

	cardKind := contracts.CardKind(kind)
	card := contracts.Card{
		ID:          cardUUID,
		SetId:       setUUID,
		Kind:        cardKind,
		Question:    question,
		Explanation: explanation,
		SourceQuote: sourceQuote,
		Topic:       topic,
	}

	if sourceRef.Valid && sourceRef.String != "" {
		card.SourceRef.SetTo(sourceRef.String)
	}

	ansStr := ""
	if answerText.Valid {
		ansStr = answerText.String
	}

	// Populate options for choice cards
	switch cardKind {
	case contracts.CardKindChoice:
		opts, err := s.querier.GetCardOptions(ctx, id)
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
		cols, err := s.querier.GetCardTableColumns(ctx, id)
		if err != nil {
			return contracts.Card{}, fmt.Errorf("get card table columns: %w", err)
		}
		items, err := s.querier.GetCardTableItems(ctx, id)
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
		// flip or input
		card.Answer = contracts.CardAnswer{
			Type:   contracts.StringCardAnswer,
			String: ansStr,
		}
	}

	return card, nil
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

func buildUserContract(id int64, firstName string, lastName, username pgtype.Text) contracts.User {
	effectiveFirstName := strings.TrimSpace(firstName)
	if effectiveFirstName == "" {
		if username.Valid && strings.TrimSpace(username.String) != "" {
			effectiveFirstName = strings.TrimSpace(username.String)
		} else {
			effectiveFirstName = fmt.Sprintf("User %d", id)
		}
	}

	u := contracts.User{
		ID:        id,
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
