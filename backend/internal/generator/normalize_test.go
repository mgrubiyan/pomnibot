package generator

import (
	"strings"
	"testing"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "hyphenated word at line end",
			in:   "Способность клеток к де-\nлению лежит в основе роста.",
			want: "Способность клеток к делению лежит в основе роста.",
		},
		{
			name: "soft hyphen at line end",
			in:   "В периоде G2 клет\u00ad\nка готовится к делению.",
			want: "В периоде G2 клетка готовится к делению.",
		},
		{
			name: "line continues with lowercase",
			in:   "Клеточный цикл состоит из интерфазы\nи собственно деления.",
			want: "Клеточный цикл состоит из интерфазы и собственно деления.",
		},
		{
			name: "long line cut after a word continues with a capital",
			in:   "В периоде G1 клетка растёт, синтезирует белки, липиды, углеводы и\nРНК, накапливает энергию.",
			want: "В периоде G1 клетка растёт, синтезирует белки, липиды, углеводы и РНК, накапливает энергию.",
		},
		{
			name: "heading and list items stay on their lines",
			in:   "2.1. Фазы митоза\nМитоз делят на фазы:\n- профаза;\n- метафаза;\nа) анафаза",
			want: "2.1. Фазы митоза\nМитоз делят на фазы:\n- профаза;\n- метафаза;\nа) анафаза",
		},
		{
			name: "spaces: nbsp, tabs, runs, zero-width",
			in:   "около 90\u00a0%\tвремени   цикла\u200b.",
			want: "около 90 % времени цикла.",
		},
		{
			name: "CRLF and blank lines",
			in:   "\r\n\r\nПервый абзац.\r\n\r\n\r\n\r\nВторой абзац.\r\n",
			want: "Первый абзац.\n\nВторой абзац.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeText(tt.in); got != tt.want {
				t.Errorf("normalizeText()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestFindQuoteLimitsTheGap(t *testing.T) {
	filler := strings.Repeat("Эта фраза нужна только для объёма и ни о чём не говорит. ", 10)
	fragment := "Ядро окружено ядерной оболочкой из двух мембран. " + filler +
		"Клетки печени делятся амитозом при повреждении ткани."
	quote := "Ядро окружено ядерной оболочкой из двух мембран... Клетки печени делятся амитозом при повреждении ткани."
	if got, ok := findQuote(fragment, quote); ok {
		t.Errorf("findQuote() accepted pieces %d characters apart as %q", len([]rune(filler)), got)
	}
}

func TestFindQuote(t *testing.T) {
	const fragment = "Клеточный цикл — это период жизни клетки от одного деления до следующего\n" +
		"или до её гибели. Ядро окружено «ядерной оболочкой» из двух мембран. " +
		"Интерфаза делится на пресинтетический (G1) и синтетический (S) периоды. " +
		"Считать, что клетка растёт всегда, неверно, что бы ни говорили. " +
		"Клетки печени делятся амитозом при повреждении ткани. " +
		"Раствор замерзает при температуре -5 °C и ниже."

	tests := []struct {
		name  string
		quote string
		want  string // "" means the quote must be rejected
	}{
		{
			name:  "exact",
			quote: "Клеточный цикл — это период жизни клетки",
			want:  "Клеточный цикл — это период жизни клетки",
		},
		{
			name:  "case, ё, spaces, line break and dash differ",
			quote: "клеточный  цикл - это период жизни клетки от одного деления до следующего или до ее гибели.",
			want: "Клеточный цикл — это период жизни клетки от одного деления до следующего\n" +
				"или до её гибели",
		},
		{
			name:  "other quote marks",
			quote: `Ядро окружено "ядерной оболочкой" из двух мембран`,
			want:  "Ядро окружено «ядерной оболочкой» из двух мембран",
		},
		{
			name:  "brackets are kept, the final period is not",
			quote: "Интерфаза делится на пресинтетический (G1) и синтетический (S)",
			want:  "Интерфаза делится на пресинтетический (G1) и синтетический (S)",
		},
		{
			name:  "starts inside a word and flips the meaning",
			quote: "верно, что бы ни говорили",
		},
		{
			name:  "starts inside a word and names another term",
			quote: "митозом при повреждении ткани",
		},
		{
			name:  "ends inside a word",
			quote: "Клетки печени делятся амито",
		},
		{
			name:  "number sign is part of the quote",
			quote: "Раствор замерзает при температуре -5 °C и ниже.",
			want:  "Раствор замерзает при температуре -5 °C и ниже",
		},
		{
			name:  "paraphrase",
			quote: "Клеточный цикл — это время жизни клетки между двумя делениями",
		},
		{
			name:  "ellipsis cutting a sentence short",
			quote: "Клеточный цикл — это период … до её гибели",
		},
		{
			name:  "sentences joined with an ellipsis: the whole passage",
			quote: "Ядро окружено «ядерной оболочкой» из двух мембран... Считать, что клетка растёт всегда, неверно",
			want: "Ядро окружено «ядерной оболочкой» из двух мембран. " +
				"Интерфаза делится на пресинтетический (G1) и синтетический (S) периоды. " +
				"Считать, что клетка растёт всегда, неверно",
		},
		{
			name:  "a sentence left out silently: the whole passage",
			quote: "Ядро окружено «ядерной оболочкой» из двух мембран. Считать, что клетка растёт всегда, неверно, что бы ни говорили.",
			want: "Ядро окружено «ядерной оболочкой» из двух мембран. " +
				"Интерфаза делится на пресинтетический (G1) и синтетический (S) периоды. " +
				"Считать, что клетка растёт всегда, неверно, что бы ни говорили",
		},
		{
			name:  "pieces out of order",
			quote: "Клетки печени делятся амитозом при повреждении ткани... Ядро окружено «ядерной оболочкой» из двух мембран",
		},
		{
			name:  "one of the pieces is a paraphrase",
			quote: "Ядро окружено «ядерной оболочкой» из двух мембран. Клетки печени делятся митозом при повреждении ткани.",
		},
		{
			name:  "too short to prove anything",
			quote: "Клеточный цикл",
		},
		{
			name:  "text from elsewhere",
			quote: "Митоз — непрямое деление соматических клеток.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findQuote(fragment, tt.quote)
			if tt.want == "" {
				if ok {
					t.Fatalf("findQuote() accepted %q as %q", tt.quote, got)
				}
				return
			}
			if !ok {
				t.Fatalf("findQuote() rejected %q", tt.quote)
			}
			if got != tt.want {
				t.Errorf("findQuote() span\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestTrimQuote(t *testing.T) {
	for in, want := range map[string]string{
		"клетка делится.":    "клетка делится",
		"-это деление;":      "это деление",
		" -5 °c и ниже. ":    "-5 °c и ниже",
		"(g1) и (s)":         "(g1) и (s)",
		".,;":                "",
		"стадия g2 (синтез)": "стадия g2 (синтез)",
	} {
		if got := trimQuote(in); got != want {
			t.Errorf("trimQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
