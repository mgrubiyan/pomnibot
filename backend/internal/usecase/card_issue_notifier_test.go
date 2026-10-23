package usecase_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestChatIssueNotifier(t *testing.T) {
	account := usecase.Account{User: contracts.User{ID: 11}, MaxID: 5, TgID: 6}
	accounts := accountsFunc(func(_ context.Context, id int64) (usecase.Account, error) {
		if id != 11 {
			t.Errorf("account of %d", id)
		}
		return account, nil
	})
	failing, working := &messengerSpy{err: errors.New("blocked")}, &messengerSpy{}
	n := usecase.NewChatIssueNotifier(accounts, failing, working)
	ctx, cancel := context.WithCancel(context.Background())
	n.CardIssueReported(ctx, usecase.CardIssueReport{
		AuthorID: 11, SetTitle: "Биология", Question: "Что такое митоз?", Reason: contracts.CardIssueReasonNotInNotes,
	})
	// The request is over by the time the message goes out.
	cancel()
	n.Wait()

	for _, m := range []*messengerSpy{failing, working} {
		if len(m.sent) != 1 || m.sent[0].account != account {
			t.Fatalf("sent = %+v", m.sent)
		}
		if m.ctxErr != nil {
			t.Errorf("sent with a cancelled context: %v", m.ctxErr)
		}
	}
	text := working.sent[0].text
	for _, want := range []string{"«Биология»", "Что такое митоз?", "этого нет в конспекте", "«Жалобы»"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}
}

func TestChatIssueNotifier_NoAccount(t *testing.T) {
	m := &messengerSpy{}
	n := usecase.NewChatIssueNotifier(accountsFunc(func(context.Context, int64) (usecase.Account, error) {
		return usecase.Account{}, errors.New("db down")
	}), m)
	n.CardIssueReported(context.Background(), usecase.CardIssueReport{AuthorID: 11})
	n.Wait()
	if len(m.sent) != 0 {
		t.Fatalf("sent = %+v", m.sent)
	}
}

func TestCardIssueReport_TextCutsLongQuestion(t *testing.T) {
	text := usecase.CardIssueReport{SetTitle: "S", Question: strings.Repeat("я", 500), Reason: contracts.CardIssueReasonOther}.Text()
	if strings.Count(text, "я") > 200 || !strings.Contains(text, "…") {
		t.Fatalf("text = %q", text)
	}
}

type accountsFunc func(context.Context, int64) (usecase.Account, error)

func (f accountsFunc) GetAccount(ctx context.Context, id int64) (usecase.Account, error) {
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
