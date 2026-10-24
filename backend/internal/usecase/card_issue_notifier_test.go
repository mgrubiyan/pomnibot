package usecase_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestChatIssueNotifier(t *testing.T) {
	authorID := uuid.New()
	account := usecase.Account{User: contracts.User{ID: authorID}, MaxID: 5, TgID: 6}
	accounts := accountsFunc(func(_ context.Context, id uuid.UUID) (usecase.Account, error) {
		if id != authorID {
			t.Errorf("account of %s", id)
		}
		return account, nil
	})
	failing, working := &messengerSpy{err: errors.New("blocked")}, &messengerSpy{}
	n := usecase.NewChatIssueNotifier(accounts, failing, working)
	ctx, cancel := context.WithCancel(context.Background())
	n.CardIssueReported(ctx, usecase.CardIssueReport{AuthorID: authorID, SetTitle: "Биология"})
	// The request is over by the time the message goes out.
	cancel()
	n.Close()

	for _, m := range []*messengerSpy{failing, working} {
		if len(m.sent) != 1 || m.sent[0].account != account {
			t.Fatalf("sent = %+v", m.sent)
		}
		if m.ctxErr != nil {
			t.Errorf("sent with a cancelled context: %v", m.ctxErr)
		}
	}
	if text := working.sent[0].text; !strings.Contains(text, "«Биология»") || !strings.Contains(text, "«Жалобы»") {
		t.Errorf("text = %q", text)
	}
}

func TestChatIssueNotifier_NoAccount(t *testing.T) {
	authorID := uuid.New()
	m := &messengerSpy{}
	n := usecase.NewChatIssueNotifier(accountsFunc(func(context.Context, uuid.UUID) (usecase.Account, error) {
		return usecase.Account{}, errors.New("db down")
	}), m)
	n.CardIssueReported(context.Background(), usecase.CardIssueReport{AuthorID: authorID})
	n.Close()
	if len(m.sent) != 0 {
		t.Fatalf("sent = %+v", m.sent)
	}
}

type accountsFunc func(context.Context, uuid.UUID) (usecase.Account, error)

func (f accountsFunc) GetAccount(ctx context.Context, id uuid.UUID) (usecase.Account, error) {
	return f(ctx, id)
}

type sentMessage struct {
	account usecase.Account
	text    string
}

type messengerSpy struct {
	mu     sync.Mutex
	err    error
	sent   []sentMessage
	ctxErr error
}

func (m *messengerSpy) SendToUser(ctx context.Context, account usecase.Account, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMessage{account, text})
	m.ctxErr = ctx.Err()
	return m.err
}

// A complaint saved while the server stops is kept, but no message starts
// once Close has begun waiting.
func TestChatIssueNotifier_AfterClose(t *testing.T) {
	authorID := uuid.New()
	m := &messengerSpy{}
	n := usecase.NewChatIssueNotifier(accountsFunc(func(context.Context, uuid.UUID) (usecase.Account, error) {
		return usecase.Account{MaxID: 5}, nil
	}), m)
	n.Close()
	n.CardIssueReported(context.Background(), usecase.CardIssueReport{AuthorID: authorID})
	n.Close()
	if len(m.sent) != 0 {
		t.Fatalf("sent after Close: %+v", m.sent)
	}
}
