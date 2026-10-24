package notes

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestFileCountText(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  string
	}{
		{0, "0 файлов"},
		{1, "1 файл"},
		{2, "2 файла"},
		{4, "4 файла"},
		{5, "5 файлов"},
		{10, "10 файлов"},
		{11, "11 файлов"},
		{12, "12 файлов"},
		{14, "14 файлов"},
		{20, "20 файлов"},
		{21, "21 файл"},
		{22, "22 файла"},
		{25, "25 файлов"},
		{34, "34 файла"},
		{100, "100 файлов"},
		{101, "101 файл"},
		{104, "104 файла"},
		{111, "111 файлов"},
		{112, "112 файлов"},
		{114, "114 файлов"},
		{121, "121 файл"},
	} {
		t.Run(strconv.Itoa(tc.count), func(t *testing.T) {
			for name, text := range map[string]string{
				"received": ReceivedText(tc.count),
				"queued":   QueuedText(tc.count, 2),
			} {
				if want := "📸 Получил " + tc.want + ". "; !strings.HasPrefix(text, want) {
					t.Errorf("%s text = %q, want prefix %q", name, text, want)
				}
			}
		})
	}
}

func TestBudgetFailureDoesNotInviteRepeatingTheSamePaidDocument(t *testing.T) {
	got := FailureText(fmt.Errorf("generation: %w", providers.ErrBudget))
	if got != TextBudgetExceeded {
		t.Fatalf("budget cause concealed: %q", got)
	}
}

func TestRefusalFailureAsksForOtherNotes(t *testing.T) {
	got := FailureText(fmt.Errorf("generate cards: %w", providers.ErrRefused))
	if got != TextOtherText {
		t.Fatalf("refusal cause concealed: %q", got)
	}
}

func TestSuccessText(t *testing.T) {
	for _, warnings := range [][]string{nil, {"Страница 2 размыта", "Пропущен слайд"}} {
		result := Result{Set: &contracts.CardSet{Title: "Лекция", ShareCode: contracts.NewOptString("123456")}, Warnings: warnings}
		want := "🎉 Набор «Лекция» успешно создан!\nКод для совместного доступа: 123456"
		if len(warnings) > 0 {
			want += "\n\n⚠️ Есть замечания к набору — их можно посмотреть в мини-приложении."
		}
		want += "\n\nОткрой мини-приложение, чтобы начать тренировку:"
		if got := SuccessText(result); got != want {
			t.Errorf("SuccessText = %q, want %q", got, want)
		}
	}
}
