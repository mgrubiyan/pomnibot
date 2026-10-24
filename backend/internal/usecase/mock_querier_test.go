package usecase_test

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

type mockQuerier struct {
	db.Querier
	countTotalDueCardsForUserFunc func(ctx context.Context, userID pgtype.UUID) (int32, error)
	getNextReviewDateForUserFunc  func(ctx context.Context, userID pgtype.UUID) (pgtype.Timestamptz, error)
	createCardFunc                func(ctx context.Context, arg db.CreateCardParams) (db.Card, error)
	createCardOptionFunc          func(ctx context.Context, arg db.CreateCardOptionParams) (db.CardOption, error)
	createFactFunc                func(ctx context.Context, arg db.CreateFactParams) (db.Fact, error)
	createCardTableColumnFunc     func(ctx context.Context, arg db.CreateCardTableColumnParams) (db.CardTableColumn, error)
	createCardTableItemFunc       func(ctx context.Context, arg db.CreateCardTableItemParams) (db.CardTableItem, error)
	createSetFunc                 func(ctx context.Context, arg db.CreateSetParams) (db.Set, error)
	deleteCardFunc                func(ctx context.Context, arg db.DeleteCardParams) (int64, error)
	deleteCardOptionsFunc         func(ctx context.Context, cardID pgtype.UUID) error
	deleteCardTableColumnsFunc    func(ctx context.Context, cardID pgtype.UUID) error
	deleteCardTableItemsFunc      func(ctx context.Context, cardID pgtype.UUID) error
	deleteSetFunc                 func(ctx context.Context, arg db.DeleteSetParams) (int64, error)
	getCardByIDFunc               func(ctx context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error)
	getCardByIDForAuthorFunc      func(ctx context.Context, arg db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error)
	getCardOptionsFunc            func(ctx context.Context, cardID pgtype.UUID) ([]db.CardOption, error)
	getCardTableColumnsFunc       func(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableColumn, error)
	getCardTableItemsFunc         func(ctx context.Context, cardID pgtype.UUID) ([]db.CardTableItem, error)
	getCardsBySetIDFunc           func(ctx context.Context, arg db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error)
	leaveSetFunc                  func(ctx context.Context, arg db.LeaveSetParams) (int64, error)
	getFeedCardsForUserFunc       func(ctx context.Context, userID pgtype.UUID) ([]db.GetFeedCardsForUserRow, error)
	getSetByIDFunc                func(ctx context.Context, arg db.GetSetByIDParams) (db.GetSetByIDRow, error)
	getSetByShareCodeFunc         func(ctx context.Context, shareCode string) (db.GetSetByShareCodeRow, error)
	getSetPlanFunc                func(ctx context.Context, arg db.GetSetPlanParams) ([]db.GetSetPlanRow, error)
	getUserByIDFunc               func(ctx context.Context, id pgtype.UUID) (db.User, error)
	getUserSetsFunc               func(ctx context.Context, userID pgtype.UUID) ([]db.GetUserSetsRow, error)
	initUserFactProgressFunc      func(ctx context.Context, arg db.InitUserFactProgressParams) error
	isSetAuthorFunc               func(ctx context.Context, arg db.IsSetAuthorParams) (bool, error)
	joinSetFunc                   func(ctx context.Context, arg db.JoinSetParams) (db.UserSet, error)
	recordAnswerFunc              func(ctx context.Context, arg db.RecordAnswerParams) (db.Answer, error)
	updateCardFunc                func(ctx context.Context, arg db.UpdateCardParams) (db.UpdateCardRow, error)
	countCardsByFactIDFunc        func(ctx context.Context, factID pgtype.UUID) (int32, error)
	getUserFactProgressFunc       func(ctx context.Context, arg db.GetUserFactProgressParams) (db.UserFactProgress, error)
	upsertFactProgressFunc        func(ctx context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error)
	ensureUserFunc                func(ctx context.Context, id pgtype.UUID) (db.User, error)
	getSetLeaderboardFunc         func(ctx context.Context, setID pgtype.UUID) ([]db.GetSetLeaderboardRow, error)
	getUserSetRatingFunc          func(ctx context.Context, arg db.GetUserSetRatingParams) (db.GetUserSetRatingRow, error)
	getUsersWithDueFactsFunc      func(ctx context.Context, arg db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error)
	createCardIssueFunc            func(ctx context.Context, arg db.CreateCardIssueParams) (db.CreateCardIssueRow, error)
	getSetIssuesFunc               func(ctx context.Context, arg db.GetSetIssuesParams) ([]db.GetSetIssuesRow, error)
	closeCardIssuesFunc            func(ctx context.Context, arg db.CloseCardIssuesParams) (bool, error)
	listCardIssuesFunc             func(ctx context.Context) ([]db.ListCardIssuesRow, error)
	rollbackFactStepOnCardIssueFunc func(ctx context.Context, arg db.RollbackFactStepOnCardIssueParams) error
}

func (m *mockQuerier) ListCardIssues(ctx context.Context) ([]db.ListCardIssuesRow, error) {
	if m.listCardIssuesFunc != nil {
		return m.listCardIssuesFunc(ctx)
	}
	return nil, nil
}

func (m *mockQuerier) GetSetIssues(ctx context.Context, arg db.GetSetIssuesParams) ([]db.GetSetIssuesRow, error) {
	if m.getSetIssuesFunc != nil {
		return m.getSetIssuesFunc(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) CloseCardIssues(ctx context.Context, arg db.CloseCardIssuesParams) (bool, error) {
	if m.closeCardIssuesFunc != nil {
		return m.closeCardIssuesFunc(ctx, arg)
	}
	return false, nil
}

func (m *mockQuerier) CreateCardIssue(ctx context.Context, arg db.CreateCardIssueParams) (db.CreateCardIssueRow, error) {
	if m.createCardIssueFunc != nil {
		return m.createCardIssueFunc(ctx, arg)
	}
	return db.CreateCardIssueRow{}, nil
}

func (m *mockQuerier) RollbackFactStepOnCardIssue(ctx context.Context, arg db.RollbackFactStepOnCardIssueParams) error {
	if m.rollbackFactStepOnCardIssueFunc != nil {
		return m.rollbackFactStepOnCardIssueFunc(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) GetUsersWithDueFacts(ctx context.Context, arg db.GetUsersWithDueFactsParams) ([]pgtype.UUID, error) {
	if m.getUsersWithDueFactsFunc != nil {
		return m.getUsersWithDueFactsFunc(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) CountTotalDueCardsForUser(ctx context.Context, userID pgtype.UUID) (int32, error) {
	if m.countTotalDueCardsForUserFunc != nil {
		return m.countTotalDueCardsForUserFunc(ctx, userID)
	}
	return 0, nil
}

func (m *mockQuerier) GetNextReviewDateForUser(ctx context.Context, userID pgtype.UUID) (pgtype.Timestamptz, error) {
	if m.getNextReviewDateForUserFunc != nil {
		return m.getNextReviewDateForUserFunc(ctx, userID)
	}
	return pgtype.Timestamptz{}, nil
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

func (m *mockQuerier) CreateFact(ctx context.Context, arg db.CreateFactParams) (db.Fact, error) {
	if m.createFactFunc != nil {
		return m.createFactFunc(ctx, arg)
	}
	return db.Fact{ID: arg.ID, SetID: arg.SetID, Name: arg.Name}, nil
}

func (m *mockQuerier) CreateCardTableColumn(ctx context.Context, arg db.CreateCardTableColumnParams) (db.CardTableColumn, error) {
	if m.createCardTableColumnFunc != nil {
		return m.createCardTableColumnFunc(ctx, arg)
	}
	return db.CardTableColumn{}, nil
}

func (m *mockQuerier) CreateCardTableItem(ctx context.Context, arg db.CreateCardTableItemParams) (db.CardTableItem, error) {
	if m.createCardTableItemFunc != nil {
		return m.createCardTableItemFunc(ctx, arg)
	}
	return db.CardTableItem{}, nil
}

func (m *mockQuerier) CreateSet(ctx context.Context, arg db.CreateSetParams) (db.Set, error) {
	if m.createSetFunc != nil {
		return m.createSetFunc(ctx, arg)
	}
	return db.Set{}, nil
}

func (m *mockQuerier) DeleteCard(ctx context.Context, arg db.DeleteCardParams) (int64, error) {
	if m.deleteCardFunc != nil {
		return m.deleteCardFunc(ctx, arg)
	}
	return 1, nil
}

func (m *mockQuerier) DeleteCardOptions(ctx context.Context, cardID pgtype.UUID) error {
	if m.deleteCardOptionsFunc != nil {
		return m.deleteCardOptionsFunc(ctx, cardID)
	}
	return nil
}

func (m *mockQuerier) DeleteCardTableColumns(ctx context.Context, cardID pgtype.UUID) error {
	if m.deleteCardTableColumnsFunc != nil {
		return m.deleteCardTableColumnsFunc(ctx, cardID)
	}
	return nil
}

func (m *mockQuerier) DeleteCardTableItems(ctx context.Context, cardID pgtype.UUID) error {
	if m.deleteCardTableItemsFunc != nil {
		return m.deleteCardTableItemsFunc(ctx, cardID)
	}
	return nil
}

func (m *mockQuerier) DeleteSet(ctx context.Context, arg db.DeleteSetParams) (int64, error) {
	if m.deleteSetFunc != nil {
		return m.deleteSetFunc(ctx, arg)
	}
	return 1, nil
}

func (m *mockQuerier) GetCardByID(ctx context.Context, arg db.GetCardByIDParams) (db.GetCardByIDRow, error) {
	if m.getCardByIDFunc != nil {
		return m.getCardByIDFunc(ctx, arg)
	}
	return db.GetCardByIDRow{}, nil
}

func (m *mockQuerier) GetCardByIDForAuthor(ctx context.Context, arg db.GetCardByIDForAuthorParams) (db.GetCardByIDForAuthorRow, error) {
	if m.getCardByIDForAuthorFunc != nil {
		return m.getCardByIDForAuthorFunc(ctx, arg)
	}
	return db.GetCardByIDForAuthorRow{}, nil
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

func (m *mockQuerier) GetCardsBySetID(ctx context.Context, arg db.GetCardsBySetIDParams) ([]db.GetCardsBySetIDRow, error) {
	if m.getCardsBySetIDFunc != nil {
		return m.getCardsBySetIDFunc(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) GetFeedCardsForUser(ctx context.Context, userID pgtype.UUID) ([]db.GetFeedCardsForUserRow, error) {
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

func (m *mockQuerier) GetSetByShareCode(ctx context.Context, shareCode string) (db.GetSetByShareCodeRow, error) {
	if m.getSetByShareCodeFunc != nil {
		return m.getSetByShareCodeFunc(ctx, shareCode)
	}
	return db.GetSetByShareCodeRow{}, nil
}

func (m *mockQuerier) GetSetPlan(ctx context.Context, arg db.GetSetPlanParams) ([]db.GetSetPlanRow, error) {
	if m.getSetPlanFunc != nil {
		return m.getSetPlanFunc(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) GetUserByID(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if m.getUserByIDFunc != nil {
		return m.getUserByIDFunc(ctx, id)
	}
	return db.User{}, nil
}

func (m *mockQuerier) GetUserSets(ctx context.Context, userID pgtype.UUID) ([]db.GetUserSetsRow, error) {
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

func (m *mockQuerier) LeaveSet(ctx context.Context, arg db.LeaveSetParams) (int64, error) {
	if m.leaveSetFunc != nil {
		return m.leaveSetFunc(ctx, arg)
	}
	return 1, nil
}

func (m *mockQuerier) RecordAnswer(ctx context.Context, arg db.RecordAnswerParams) (db.Answer, error) {
	if m.recordAnswerFunc != nil {
		return m.recordAnswerFunc(ctx, arg)
	}
	return db.Answer{}, nil
}

func (m *mockQuerier) UpdateCard(ctx context.Context, arg db.UpdateCardParams) (db.UpdateCardRow, error) {
	if m.updateCardFunc != nil {
		return m.updateCardFunc(ctx, arg)
	}
	return db.UpdateCardRow{}, nil
}

func (m *mockQuerier) CountCardsByFactID(ctx context.Context, factID pgtype.UUID) (int32, error) {
	if m.countCardsByFactIDFunc != nil {
		return m.countCardsByFactIDFunc(ctx, factID)
	}
	return 1, nil
}

func (m *mockQuerier) GetUserFactProgress(ctx context.Context, arg db.GetUserFactProgressParams) (db.UserFactProgress, error) {
	if m.getUserFactProgressFunc != nil {
		return m.getUserFactProgressFunc(ctx, arg)
	}
	return db.UserFactProgress{}, nil
}

func (m *mockQuerier) UpsertFactProgress(ctx context.Context, arg db.UpsertFactProgressParams) (db.UserFactProgress, error) {
	if m.upsertFactProgressFunc != nil {
		return m.upsertFactProgressFunc(ctx, arg)
	}
	return db.UserFactProgress{}, nil
}

func (m *mockQuerier) EnsureUser(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, id)
	}
	return db.User{ID: id}, nil
}

func (m *mockQuerier) GetSetLeaderboard(ctx context.Context, setID pgtype.UUID) ([]db.GetSetLeaderboardRow, error) {
	if m.getSetLeaderboardFunc != nil {
		return m.getSetLeaderboardFunc(ctx, setID)
	}
	return nil, nil
}

func (m *mockQuerier) GetUserSetRating(ctx context.Context, arg db.GetUserSetRatingParams) (db.GetUserSetRatingRow, error) {
	if m.getUserSetRatingFunc != nil {
		return m.getUserSetRatingFunc(ctx, arg)
	}
	return db.GetUserSetRatingRow{Percentile: 0, Rank: 1}, nil
}

func (m *mockQuerier) GetCardOptionsBatch(ctx context.Context, ids []pgtype.UUID) ([]db.CardOption, error) {
	var rows []db.CardOption
	for _, id := range ids {
		details, err := m.GetCardOptions(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, detail := range details {
			detail.CardID = id
			rows = append(rows, detail)
		}
	}
	return rows, nil
}

func (m *mockQuerier) GetCardTableColumnsBatch(ctx context.Context, ids []pgtype.UUID) ([]db.CardTableColumn, error) {
	var rows []db.CardTableColumn
	for _, id := range ids {
		details, err := m.GetCardTableColumns(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, detail := range details {
			detail.CardID = id
			rows = append(rows, detail)
		}
	}
	return rows, nil
}

func (m *mockQuerier) GetCardTableItemsBatch(ctx context.Context, ids []pgtype.UUID) ([]db.CardTableItem, error) {
	var rows []db.CardTableItem
	for _, id := range ids {
		details, err := m.GetCardTableItems(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, detail := range details {
			detail.CardID = id
			rows = append(rows, detail)
		}
	}
	return rows, nil
}
