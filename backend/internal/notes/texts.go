package notes

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// What the student reads when a document does not become a set. Each text
// says only what the student can do next: send the file again, send a
// different one, or wait. Our own failures, such as the model or the database,
// are not explained; the student cannot fix them.
const (
	TextBudgetExceeded = "😕 Достигнут лимит обработки этого документа. Проверенных карточек пока нет. Пришли меньший фрагмент."
	TextSourceLimit    = "😕 Не получилось обработать документ целиком. Пришли отдельную главу или меньший фрагмент."
	TextTryLater       = "😕 Не получилось сделать карточки. Пришли конспект ещё раз через пару минут."
	TextResend         = "😕 Не получилось получить файл. Пришли его ещё раз."
	TextNoText         = "😕 Не нашёл в файле текста. Если это фото, сними страницу целиком, при хорошем свете и без размытия."
	TextOnlyJunk       = "😕 В этом файле нет материала для карточек. Пришли конспект лекции или страницы учебника."
	TextOtherText      = "😕 По этому тексту не получилось составить карточки. Попробуй прислать другой конспект."
	TextNoCards        = "😕 По этому тексту не получилось составить карточки. Пришли конспект подробнее: с определениями, свойствами, причинами и следствиями."
)

// maxWarningsShown keeps a long scan with many blurry pages from burying the
// message about the finished set.
const maxWarningsShown = 3

// FailureText is what to tell the student about an error of Make, or ""
// when there is nothing to tell: the student gave up waiting.
func FailureText(err error) string {
	var (
		ee *extractError
		ne *NoCardsError
	)
	switch {
	case errors.Is(err, ErrGaveUp):
		return ""
	case errors.Is(err, providers.ErrBudget):
		return TextBudgetExceeded
	case errors.Is(err, providers.ErrCapability):
		return TextSourceLimit
	case errors.Is(err, ErrNoFiles):
		return TextResend
	case errors.Is(err, ErrNoText):
		return TextNoText
	case errors.As(err, &ee):
		return extractFailure(ee.err, ee.files)
	case errors.As(err, &ne):
		return noCardsReason(ne.Stats)
	default:
		return TextTryLater
	}
}

// extractFailure explains why the files could not be read. Ingest errors
// already carry a message for the student; with several files it names the
// one at fault.
func extractFailure(err error, files int) string {
	var ie *ingest.Error
	if !errors.As(err, &ie) || ie.Message == "" {
		return TextTryLater
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
		return TextOnlyJunk
	case s.ChunksRefused > 0 && s.ChunksRefused == sent:
		return TextOtherText
	default:
		return TextNoCards
	}
}

// WithWarnings appends what went wrong on single pages, such as a blurry
// photo, to the message about a finished set.
func WithWarnings(text string, warnings []string) string {
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

// Shared progress and result wording used by every messenger adapter.
const (
	TextReading    = "📸 Твоя очередь! Распознаю текст... ⏳"
	TextGenerating = "🧠 Анализирую конспект и создаю карточки... ⏳"
)

func fileWord(count int) string {
	if lastTwo := count % 100; lastTwo >= 11 && lastTwo <= 14 {
		return "файлов"
	}
	switch count % 10 {
	case 1:
		return "файл"
	case 2, 3, 4:
		return "файла"
	default:
		return "файлов"
	}
}

// ReceivedText confirms the files before extraction.
func ReceivedText(count int) string {
	return fmt.Sprintf("📸 Получил %d %s. Распознаю текст... ⏳", count, fileWord(count))
}

// QueuedText explains the position in the shared generation queue.
func QueuedText(count, ahead int) string {
	return fmt.Sprintf("📸 Получил %d %s. Сейчас обрабатываю другие конспекты, ты в очереди, перед тобой: %d. Начну, как только освобожусь ⏳", count, fileWord(count), ahead)
}

// CardsText reports streamed card generation progress.
func CardsText(total int) string {
	return fmt.Sprintf("🧠 Создаю карточки... Уже готово: %d ⏳", total)
}

// SuccessText includes the common share code, warnings and Mini App action.
func SuccessText(result Result) string {
	return WithWarnings(fmt.Sprintf("🎉 Набор «%s» успешно создан!\nКод для совместного доступа: %s", result.Set.Title, result.Set.ShareCode.Value), result.Warnings) + "\n\nОткрой мини-приложение, чтобы начать тренировку:"
}
