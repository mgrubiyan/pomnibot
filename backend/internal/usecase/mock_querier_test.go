package usecase_test

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type mockQuerier struct {
	db.Querier
	countTotalDueCardsForUserFunc  func(ctx context.Context, userID int64) (int32, error)
	countUserActiveDaysFunc        func(ctx context.Context, userID int64) (int32, error)
	createCardFunc                 func(ctx context.Context, arg db.CreateCardParams) (db.Card, error)
	createCardIssueFunc            func(ctx context.Context, arg db.CreateCardIssueParams) (db.CardIssue, error)
	createCardOptionFunc           func(ctx context.Context, arg db.CreateCardOptionParams) (db.CardOption, error)
	createSetFunc                  func(ctx context.Context, arg db.CreateSetParams) (db.Set, error)
	deleteCardFunc                 func(ctx context.Context, id pgtype.UUID) error
	deleteSetFunc                  func(ctx context.Context, arg db.DeleteSetParams) error
	getCardByIDFunc                func(ctx context.Context, id pgtype.UUID) (db.GetCardByIDRow, error)
	getCardOptionsFunc             func(ctx context.Context, cardID pgtype.UUID) ([]db.CardOption, error)
	getCardTableColumnsFunc        func(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableColumn, error)
	getCardTableItemsFunc          func(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableItem, error)
	getCardsBySetIDFunc            func(ctx context.Context, setID pgtype.UUID) ([]db.GetCardsBySetIDRow, error)
	getFeedCardsForUserFunc        func(ctx context.Context, userID int64) ([]db.GetFeedCardsForUserRow, error)
	getSetByIDFunc                 func(ctx context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error)
	getSetByShareCodeFunc          func(ctx context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error)
	getSetPlanFunc                 func(ctx context.Context, arg db.GetSetPlanParams) ([]db.GetSetPlanRow, error)
	getUserByIDFunc                func(ctx context.Context, id int64) (db.User, error)
	getUserSetsFunc                func(ctx context.Context, userID int64) ([]db.GetUserSetsRow, error)
	initUserFactProgressFunc       func(ctx context.Context, arg db.InitUserFactProgressParams) error
	isSetAuthorFunc                func(ctx context.Context, arg db.IsSetAuthorParams) (bool, error)
	joinSetFunc                    func(ctx context.Context, arg db.JoinSetParams) (db.UserSet, error)
	leaveSetFunc                   func(ctx context.Context, arg db.LeaveSetParams) error
	recordAnswerResultFunc         func(ctx context.Context, arg db.RecordAnswerResultParams) (db.AnswerResult, error)
	updateCardFunc                 func(ctx context.Context, arg db.UpdateCardParams) (db.Card, error)
	updateFactProgressOnAnswerFunc func(ctx context.Context, arg db.UpdateFactProgressOnAnswerParams) (db.UserFactProgress, error)
	upsertUserFunc                 func(ctx context.Context, arg db.UpsertUserParams) (db.User, error)
}

func (m *mockQuerier) CountTotalDueCardsForUser(ctx context.Context, userID int64) (int32, error) {
	if m.countTotalDueCardsForUserFunc != nil {
		return m.countTotalDueCardsForUserFunc(ctx, userID)
	}
	return 0, nil
}

func (m *mockQuerier) CountUserActiveDays(ctx context.Context, userID int64) (int32, error) {
	if m.countUserActiveDaysFunc != nil {
		return m.countUserActiveDaysFunc(ctx, userID)
	}
	return 0, nil
}

func (m *mockQuerier) CreateCard(ctx context.Context, arg db.CreateCardParams) (db.Card, error) {
	if m.createCardFunc != nil {
		return m.createCardFunc(ctx, arg)
	}
	return db.Card{}, nil
}

func (m *mockQuerier) CreateCardOption(ctx context.Context, arg db.CreateCardOptionParams) (db.CardOption, error) {
	if m.createCardOptionFunc != nil {
		return m.createCardOptionFunc(ctx, arg)
	}
	return db.CardOption{}, nil
}

func (m *mockQuerier) CreateSet(ctx context.Context, arg db.CreateSetParams) (db.Set, error) {
	if m.createSetFunc != nil {
		return m.createSetFunc(ctx, arg)
	}
	return db.Set{}, nil
}

func (m *mockQuerier) CreateCardIssue(ctx context.Context, arg db.CreateCardIssueParams) (db.CardIssue, error) {
	if m.createCardIssueFunc != nil {
		return m.createCardIssueFunc(ctx, arg)
	}
	return db.CardIssue{}, nil
}

func (m *mockQuerier) DeleteCard(ctx context.Context, id pgtype.UUID) error {
	if m.deleteCardFunc != nil {
		return m.deleteCardFunc(ctx, id)
	}
	return nil
}

func (m *mockQuerier) DeleteSet(ctx context.Context, arg db.DeleteSetParams) error {
	if m.deleteSetFunc != nil {
		return m.deleteSetFunc(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) GetCardByID(ctx context.Context, id pgtype.UUID) (db.GetCardByIDRow, error) {
	if m.getCardByIDFunc != nil {
		return m.getCardByIDFunc(ctx, id)
	}
	return db.GetCardByIDRow{}, nil
}

func (m *mockQuerier) GetCardOptions(ctx context.Context, cardID pgtype.UUID) ([]db.CardOption, error) {
	if m.getCardOptionsFunc != nil {
		return m.getCardOptionsFunc(ctx, cardID)
	}
	return nil, nil
}

func (m *mockQuerier) GetCardTableColumns(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableColumn, error) {
	if m.getCardTableColumnsFunc != nil {
		return m.getCardTableColumnsFunc(ctx, cardID)
	}
	return nil, nil
}

func (m *mockQuerier) GetCardTableItems(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableItem, error) {
	if m.getCardTableItemsFunc != nil {
		return m.getCardTableItemsFunc(ctx, cardID)
	}
	return nil, nil
}

func (m *mockQuerier) GetCardsBySetID(ctx context.Context, setID pgtype.UUID) ([]db.GetCardsBySetIDRow, error) {
	if m.getCardsBySetIDFunc != nil {
		return m.getCardsBySetIDFunc(ctx, setID)
	}
	return nil, nil
}

func (m *mockQuerier) GetFeedCardsForUser(ctx context.Context, userID int64) ([]db.GetFeedCardsForUserRow, error) {
	if m.getFeedCardsForUserFunc != nil {
		return m.getFeedCardsForUserFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockQuerier) GetSetByID(ctx context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error) {
	if m.getSetByIDFunc != nil {
		return m.getSetByIDFunc(ctx, arg)
	}
	return db.GetSetByIDRow{}, nil
}

func (m *mockQuerier) GetSetByShareCode(ctx context.Context, arg db.GetSetByShareCodeParams) (db.GetSetByShareCodeRow, error) {
	if m.getSetByShareCodeFunc != nil {
		return m.getSetByShareCodeFunc(ctx, arg)
	}
	return db.GetSetByShareCodeRow{}, nil
}

func (m *mockQuerier) GetSetPlan(ctx context.Context, arg db.GetSetPlanParams) ([]db.GetSetPlanRow, error) {
	if m.getSetPlanFunc != nil {
		return m.getSetPlanFunc(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) GetUserByID(ctx context.Context, id int64) (db.User, error) {
	if m.getUserByIDFunc != nil {
		return m.getUserByIDFunc(ctx, id)
	}
	return db.User{}, nil
}

func (m *mockQuerier) GetUserSets(ctx context.Context, userID int64) ([]db.GetUserSetsRow, error) {
	if m.getUserSetsFunc != nil {
		return m.getUserSetsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockQuerier) InitUserFactProgress(ctx context.Context, arg db.InitUserFactProgressParams) error {
	if m.initUserFactProgressFunc != nil {
		return m.initUserFactProgressFunc(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) IsSetAuthor(ctx context.Context, arg db.IsSetAuthorParams) (bool, error) {
	if m.isSetAuthorFunc != nil {
		return m.isSetAuthorFunc(ctx, arg)
	}
	return false, nil
}

func (m *mockQuerier) JoinSet(ctx context.Context, arg db.JoinSetParams) (db.UserSet, error) {
	if m.joinSetFunc != nil {
		return m.joinSetFunc(ctx, arg)
	}
	return db.UserSet{}, nil
}

func (m *mockQuerier) LeaveSet(ctx context.Context, arg db.LeaveSetParams) error {
	if m.leaveSetFunc != nil {
		return m.leaveSetFunc(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) RecordAnswerResult(ctx context.Context, arg db.RecordAnswerResultParams) (db.AnswerResult, error) {
	if m.recordAnswerResultFunc != nil {
		return m.recordAnswerResultFunc(ctx, arg)
	}
	return db.AnswerResult{}, nil
}

func (m *mockQuerier) UpdateCard(ctx context.Context, arg db.UpdateCardParams) (db.Card, error) {
	if m.updateCardFunc != nil {
		return m.updateCardFunc(ctx, arg)
	}
	return db.Card{}, nil
}

func (m *mockQuerier) UpdateFactProgressOnAnswer(ctx context.Context, arg db.UpdateFactProgressOnAnswerParams) (db.UserFactProgress, error) {
	if m.updateFactProgressOnAnswerFunc != nil {
		return m.updateFactProgressOnAnswerFunc(ctx, arg)
	}
	return db.UserFactProgress{}, nil
}

func (m *mockQuerier) UpsertUser(ctx context.Context, arg db.UpsertUserParams) (db.User, error) {
	if m.upsertUserFunc != nil {
		return m.upsertUserFunc(ctx, arg)
	}
	return db.User{ID: arg.ID, Name: arg.Name}, nil
}
