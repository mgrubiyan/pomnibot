package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

// errNotInFeed: the card is not the one the feed asks of the reader now. A
// conflict, not a missing card: the mini app skips such a card, while a card
// the reader cannot see at all (as after their sign-in expired) stops the run.
var errNotInFeed = fmt.Errorf("%w: the card is not in the feed", ErrConflict)

// replayWindow is how long a recorded verdict is repeated to a card that
// left the feed: long enough for a lost response to be retried, too short
// for an answer from an earlier pass to stand in for a new one.
const replayWindow = 2 * time.Minute

// answerLocks serialize the answers of one reader to one fact, so a double
// tap or an overlapping retry is not recorded twice. Each reader and fact
// has its own lock, so nobody waits for another reader's model call. One
// process serves the API, so locks in memory are enough.
type answerLocks struct {
	mu   sync.Mutex
	held map[answerKey]*answerLock
}

type answerKey struct {
	user uuid.UUID
	fact [16]byte
}

// answerLock is forgotten once nobody holds or waits for it.
type answerLock struct {
	mu    sync.Mutex
	users int
}

func (l *answerLocks) lock(userID uuid.UUID, factID pgtype.UUID) func() {
	key := answerKey{user: userID, fact: factID.Bytes}
	l.mu.Lock()
	if l.held == nil {
		l.held = make(map[answerKey]*answerLock)
	}
	k := l.held[key]
	if k == nil {
		k = &answerLock{}
		l.held[key] = k
	}
	k.users++
	l.mu.Unlock()

	k.mu.Lock()
	return func() {
		k.mu.Unlock()
		l.mu.Lock()
		k.users--
		if k.users == 0 {
			delete(l.held, key)
		}
		l.mu.Unlock()
	}
}

// AnswerQuestion grades and records the answer to the card the feed asks of
// the reader now. Once it is recorded the card leaves the feed, and asking
// again repeats the recorded verdict instead of grading anew.
func (s *cardServiceImpl) AnswerQuestion(ctx context.Context, userID uuid.UUID, cardID uuid.UUID, answer string) (*contracts.AnswerQuestionResponse, error) {
	if err := s.userService.EnsureUser(ctx, userID); err != nil {
		return nil, err
	}
	card, details, err := s.readerCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}

	unlock := s.locks.lock(userID, card.FactID)
	defer unlock()

	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	inFeed, err := s.inFeed(ctx, pgUserID, card.ID)
	if err != nil {
		return nil, err
	}
	if !inFeed {
		correct, err := s.lastAnswer(ctx, pgUserID, card.ID)
		if err != nil {
			return nil, err
		}
		return &contracts.AnswerQuestionResponse{
			IsCorrect: correct,
			Method:    contracts.AnswerQuestionResponseMethodLocal,
			Reveal:    contracts.NewOptCardReveal(revealOf(card, details)),
		}, nil
	}

	v, err := s.grade(ctx, card, details, answer)
	if err != nil {
		return nil, err
	}
	res := &contracts.AnswerQuestionResponse{
		IsCorrect: v.Correct,
		Method:    contracts.AnswerQuestionResponseMethod(v.Method),
	}
	if v.Method == grader.MethodFallback {
		// No verdict yet: nothing is recorded and the card stays in the
		// feed, so the reader can ask again.
		return res, nil
	}

	// Recorded together with the fact's next review, so a failed write
	// leaves the card in the feed with nothing recorded for the retry.
	if err := s.inTx(ctx, func(q db.Querier) error {
		if _, err := q.RecordAnswer(ctx, db.RecordAnswerParams{
			UserID:  pgUserID,
			CardID:  card.ID,
			Correct: v.Correct,
		}); err != nil {
			return fmt.Errorf("record answer: %w", err)
		}
		if err := processFactAnswer(ctx, q, pgUserID, card.FactID, v.Correct); err != nil {
			return fmt.Errorf("process fact answer: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if v.Reason != "" {
		res.Reason = contracts.NewOptString(v.Reason)
	}
	res.Reveal = contracts.NewOptCardReveal(revealOf(card, details))
	return res, nil
}

// RevealCard shows the answer of a flip card, which the reader grades
// themselves after seeing it. Other kinds are revealed by their answer.
func (s *cardServiceImpl) RevealCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (*contracts.CardReveal, error) {
	card, details, err := s.readerCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}
	if contracts.CardKind(card.Kind) != contracts.CardKindFlip {
		return nil, fmt.Errorf("%w: only a flip card is revealed before the answer", ErrValidation)
	}
	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}
	inFeed, err := s.inFeed(ctx, pgUserID, card.ID)
	if err != nil {
		return nil, err
	}
	if !inFeed {
		if _, err := s.lastAnswer(ctx, pgUserID, card.ID); err != nil {
			return nil, err
		}
	}
	reveal := revealOf(card, details)
	return &reveal, nil
}

// readerCard is a card of a set the user belongs to, with its options or
// table.
func (s *cardServiceImpl) readerCard(ctx context.Context, userID uuid.UUID, cardID uuid.UUID) (storedCard, cardDetails, error) {
	card, err := s.querier.GetCardByID(ctx, db.GetCardByIDParams{
		ID:     pgtype.UUID{Bytes: cardID, Valid: true},
		UserID: pgtype.UUID{Bytes: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return card, cardDetails{}, ErrNotFound
	}
	if err != nil {
		return card, cardDetails{}, fmt.Errorf("get card by id: %w", err)
	}
	details, err := loadDetails(ctx, s.querier, card)
	return card, details, err
}

// inFeed tells whether the feed asks this card of the reader now: its fact
// is due and the card is the one for the reader's step.
func (s *cardServiceImpl) inFeed(ctx context.Context, userID, cardID pgtype.UUID) (bool, error) {
	rows, err := s.querier.GetFeedCardsForUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get feed cards for user: %w", err)
	}
	for _, r := range rows {
		if r.ID == cardID {
			return true, nil
		}
	}
	return false, nil
}

// inTx runs the writes in one transaction, or straight on the querier when
// the service has no transactions.
func (s *cardServiceImpl) inTx(ctx context.Context, fn func(db.Querier) error) error {
	if s.tx == nil {
		return fn(s.querier)
	}
	return s.tx.InTx(ctx, fn)
}

// lastAnswer is the verdict recorded for the reader's answer to the card
// within the replay window; errNotInFeed when there is none.
func (s *cardServiceImpl) lastAnswer(ctx context.Context, userID, cardID pgtype.UUID) (bool, error) {
	correct, err := s.querier.GetLastAnswer(ctx, db.GetLastAnswerParams{
		UserID: userID,
		CardID: cardID,
		Since:  pgtype.Timestamptz{Time: time.Now().Add(-replayWindow), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errNotInFeed
	}
	if err != nil {
		return false, fmt.Errorf("get last answer: %w", err)
	}
	return correct, nil
}

func (s *cardServiceImpl) grade(ctx context.Context, card storedCard, details cardDetails, answer string) (grader.Verdict, error) {
	local := func(correct bool) grader.Verdict {
		return grader.Verdict{Correct: correct, Method: grader.MethodLocal}
	}
	switch contracts.CardKind(card.Kind) {
	case contracts.CardKindFlip:
		knew, err := strconv.ParseBool(answer)
		if err != nil {
			return grader.Verdict{}, fmt.Errorf("%w: a flip card is answered true or false", ErrValidation)
		}
		return local(knew), nil
	case contracts.CardKindBoolean:
		given, err := strconv.ParseBool(answer)
		if err != nil {
			return grader.Verdict{}, fmt.Errorf("%w: a true-or-false card is answered true or false", ErrValidation)
		}
		want, _ := strconv.ParseBool(card.AnswerText.String)
		return local(given == want), nil
	case contracts.CardKindChoice:
		// The options the reveal shows as right, so the verdict agrees with it.
		right, err := json.Marshal(choiceAnswer(details.options, card.AnswerText.String).StringArray)
		if err != nil {
			return grader.Verdict{}, fmt.Errorf("marshal choice answer: %w", err)
		}
		return local(cards.MatchChoiceAnswer(string(right), answer)), nil
	case contracts.CardKindTable:
		var placed []contracts.TableItem
		if err := json.Unmarshal([]byte(answer), &placed); err != nil {
			return grader.Verdict{}, fmt.Errorf("%w: a table card is answered with the terms and their columns", ErrValidation)
		}
		return local(tableMatches(details.table, placed)), nil
	default:
		return s.grader.Check(ctx, grader.Input{
			Question: card.Question,
			Expected: card.AnswerText.String,
			Quote:    card.SourceQuote,
			Given:    answer,
		}), nil
	}
}

// tableMatches: every term of the layout is placed in its column, as many
// times as the layout has it there.
func tableMatches(want contracts.TableLayout, placed []contracts.TableItem) bool {
	if len(placed) != len(want.Items) {
		return false
	}
	left := make(map[contracts.TableItem]int, len(want.Items))
	for _, item := range want.Items {
		left[item]++
	}
	for _, item := range placed {
		if left[item] == 0 {
			return false
		}
		left[item]--
	}
	return true
}
