package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CardIssueReport tells the author of a set that a reader complained about
// one of its cards. The details are in the mini app; the reader stays
// anonymous.
type CardIssueReport struct {
	AuthorID int64
	SetTitle string
}

// IssueNotifier passes a complaint on to the author of the set.
type IssueNotifier interface {
	CardIssueReported(ctx context.Context, report CardIssueReport)
}

// Messenger sends a user a message with a button to the mini app in one chat
// app. A user who can't get messages there is skipped without an error.
type Messenger interface {
	SendToUser(ctx context.Context, account Account, text string) error
}

// AccountGetter finds a user's chat accounts.
type AccountGetter interface {
	GetAccount(ctx context.Context, userID int64) (Account, error)
}

// WithIssueNotifier tells the author about complaints on their cards.
func WithIssueNotifier(n IssueNotifier) CardServiceOption {
	return func(s *cardServiceImpl) { s.issues = n }
}

// issueSendTimeout bounds one notification across all chat apps.
const issueSendTimeout = 30 * time.Second

// Text is the message to the author.
func (r CardIssueReport) Text() string {
	return fmt.Sprintf("⚠️ На карточку в наборе «%s» пожаловались. Откройте набор и нажмите «Жалобы».", r.SetTitle)
}

// ChatIssueNotifier sends complaints to the author in every chat app they
// use. Sending runs in the background, so a slow chat API never holds up the
// complaint; Wait joins the messages still going out.
type ChatIssueNotifier struct {
	accounts   AccountGetter
	messengers []Messenger
	sending    sync.WaitGroup
}

// NewChatIssueNotifier sends through messengers; with none it sends nothing.
func NewChatIssueNotifier(accounts AccountGetter, messengers ...Messenger) *ChatIssueNotifier {
	return &ChatIssueNotifier{accounts: accounts, messengers: messengers}
}

// CardIssueReported implements IssueNotifier. Failures are only logged: the
// complaint is saved, and the author still sees it in the mini app.
func (n *ChatIssueNotifier) CardIssueReported(ctx context.Context, report CardIssueReport) {
	if len(n.messengers) == 0 {
		return
	}
	// The request that saved the complaint is over by the time this runs.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issueSendTimeout)
	n.sending.Go(func() {
		defer cancel()
		account, err := n.accounts.GetAccount(ctx, report.AuthorID)
		if err != nil {
			slog.WarnContext(ctx, "card issue not sent to the author", "author_id", report.AuthorID, "error", err)
			return
		}
		text := report.Text()
		for _, m := range n.messengers {
			if err := m.SendToUser(ctx, account, text); err != nil {
				slog.WarnContext(ctx, "card issue not sent to the author", "author_id", report.AuthorID, "error", err)
			}
		}
	})
}

// Wait joins the messages still going out.
func (n *ChatIssueNotifier) Wait() { n.sending.Wait() }
