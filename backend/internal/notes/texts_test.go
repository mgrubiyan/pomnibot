package notes

import (
	"strconv"
	"strings"
	"testing"
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
