package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
)

// What the student reads when a document does not become a set. Each text
// says what went wrong and what to do next, so a wrong file is not mistaken
// for a broken bot.
const (
	msgUnavailable = "😕 Сейчас не могу разобрать конспект: сервис временно недоступен. Попробуй прислать его ещё раз позже."
	msgDownload    = "😕 Не получилось скачать файл из чата. Пришли его ещё раз."
	msgExtract     = "😕 Не получилось прочитать файл. Пришли его ещё раз или в другом формате: текст, PDF или фото страниц."
	msgNoText      = "😕 Не нашёл в файле текста. Если это фото или скан, проверь, что страница целиком в кадре, хорошо освещена и не размыта, и пришли ещё раз."
	msgModelDown   = "😕 Сервис, который составляет карточки, сейчас не отвечает. Попробуй прислать конспект ещё раз через несколько минут."
	msgOnlyJunk    = "😕 Не нашёл в тексте материала для карточек: похоже, это оглавление, список литературы или служебный текст. Пришли сам конспект."
	msgRefused     = "😕 Сервис генерации отказался обрабатывать этот текст. Попробуй прислать другой конспект."
	msgNoCards     = "😕 Не смог составить по этому тексту ни одной карточки, которую можно подтвердить цитатой из конспекта. Пришли конспект подробнее: определения, свойства, причины и следствия."
	msgSaveFailed  = "😕 Карточки готовы, но не получилось их сохранить. Пришли конспект ещё раз чуть позже."
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
		return msgExtract
	}
	if files > 1 && ie.File != "" {
		return fmt.Sprintf("😕 Файл «%s»: %s", ie.File, ie.Message)
	}
	return "😕 " + ie.Message
}

// noCardsReason explains an empty set from what the generator saw: nothing
// but junk, refusals by the content filter, or cards that did not survive
// the quote check.
func noCardsReason(s generator.Stats) string {
	sent := s.Chunks - s.ChunksFiltered
	switch {
	case s.Chunks > 0 && sent == 0:
		return msgOnlyJunk
	case s.ChunksRefused > 0 && s.ChunksRefused == sent:
		return msgRefused
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
