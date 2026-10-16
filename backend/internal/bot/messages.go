package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
)

// What the student reads when a document does not become a set. Each text
// says only what the student can do next: send the file again, send a
// different one, or wait. Our own failures, such as the model or the database,
// are not explained; the student cannot fix them.
const (
	msgTryLater  = "😕 Не получилось сделать карточки. Пришли конспект ещё раз через пару минут."
	msgResend    = "😕 Не получилось получить файл. Пришли его ещё раз."
	msgNoText    = "😕 Не нашёл в файле текста. Если это фото, сними страницу целиком, при хорошем свете и без размытия."
	msgOnlyJunk  = "😕 Не нашёл в файле материала для карточек. Пришли сам конспект с объяснениями, а не титульный лист, оглавление или список литературы."
	msgOtherText = "😕 По этому тексту не получилось составить карточки. Попробуй прислать другой конспект."
	msgNoCards   = "😕 По этому тексту не получилось составить карточки. Пришли конспект подробнее: с определениями, свойствами, причинами и следствиями."
)

// maxWarningsShown keeps a long scan with many blurry pages from burying the
// message about the finished set.
const maxWarningsShown = 3

// extractFailure explains why the files could not be read. Ingest errors
// already carry a message for the student; with several files it names the
// one at fault.
func extractFailure(err error, files int) string {
	var ie *ingest.Error
	if !errors.As(err, &ie) || ie.Message == "" {
		return msgTryLater
	}
	if files > 1 && ie.File != "" {
		return fmt.Sprintf("😕 Файл «%s»: %s", ie.File, ie.Message)
	}
	return "😕 " + ie.Message
}

// noCardsReason picks what to ask for when the set came out empty, from what
// the generator saw: nothing but junk (send the notes themselves), refusals by
// the content filter (send other notes), or cards that did not survive the
// quote check (send more detailed notes).
func noCardsReason(s generator.Stats) string {
	sent := s.Chunks - s.ChunksFiltered
	switch {
	case s.Chunks > 0 && sent == 0:
		return msgOnlyJunk
	case s.ChunksRefused > 0 && s.ChunksRefused == sent:
		return msgOtherText
	default:
		return msgNoCards
	}
}

// withWarnings appends what went wrong on single pages, such as a blurry
// photo, to the message about a finished set.
func withWarnings(text string, warnings []string) string {
	if len(warnings) == 0 {
		return text
	}
	shown := warnings[:min(len(warnings), maxWarningsShown)]
	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\n⚠️ ")
	b.WriteString(strings.Join(shown, "\n⚠️ "))
	if more := len(warnings) - len(shown); more > 0 {
		fmt.Fprintf(&b, "\n…и ещё %d", more)
	}
	return b.String()
}
