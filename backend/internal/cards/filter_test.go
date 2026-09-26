package cards

import (
	"strings"
	"testing"
)

func TestJunkReason(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "table of contents",
			text: `Содержание
Введение ......................................... 3
1. Клеточный цикл .................................. 4
1.1. Интерфаза ...................................... 4
1.2. Контрольные точки .............................. 6
2. Митоз ........................................... 7
3. Мейоз ........................................... 11
Список литературы ................................. 15`,
			want: junkTOC,
		},
		{
			name: "table of contents without leaders",
			text: `Оглавление
Введение в клеточную биологию 3
Строение и функции клеточного ядра 12
Клеточный цикл и его регуляция 27
Митоз и цитокинез у животных и растений 41
Мейоз и образование половых клеток 58`,
			want: junkTOC,
		},
		{
			name: "bibliography",
			text: `Список литературы
1. Альбертс Б., Джонсон А., Льюис Дж. Молекулярная биология клетки. — М.: Мир, 1994. — Т. 2. — 540 с.
2. Ченцов Ю. С. Введение в клеточную биологию. — М.: Академкнига, 2004. — 495 с.
3. Ярыгин В. Н. Биология: учебник. — М.: ГЭОТАР-Медиа, 2020. — 736 с. — ISBN 978-5-9704-5307-8.
4. Клеточный цикл // Большая российская энциклопедия. — URL: https://bigenc.ru (дата обращения: 01.09.2026).`,
			want: junkBibliography,
		},
		{
			name: "title page",
			text: `МИНИСТЕРСТВО НАУКИ И ВЫСШЕГО ОБРАЗОВАНИЯ РОССИЙСКОЙ ФЕДЕРАЦИИ
Казанский федеральный университет
Кафедра генетики
КОНСПЕКТ ЛЕКЦИЙ по дисциплине «Биология клетки»
Выполнила: студентка группы 01-512
Проверил: доцент Петров В. Н.`,
			want: junkTitlePage,
		},
		{
			name: "formulas and numbers",
			text: strings.Repeat(`F = m · a = 12,5 · 9,81 = 122,6 Н
E = m · c^2 = 0,002 · (3 · 10^8)^2 = 1,8 · 10^14 Дж
p = m · v = 2,4 · 15,5 = 37,2 кг·м/с
A = F · s · cos(α) = 122,6 · 4,2 · 0,87 = 448,0 Дж
P = A / t = 448,0 / 12,4 = 36,1 Вт
Ek = m · v^2 / 2 = 2,4 · 240,25 / 2 = 288,3 Дж
Ep = m · g · h = 2,4 · 9,81 · 11,8 = 277,8 Дж
`, 2),
			want: junkFewLetters,
		},
		{
			name: "page number",
			text: "Стр. 14",
			want: junkTooShort,
		},
		{
			name: "ordinary paragraph",
			text: `Митоз — непрямое деление соматических клеток, при котором из одной материнской клетки образуются две дочерние с одинаковым набором хромосом. Если материнская клетка диплоидна (2n), дочерние клетки тоже диплоидны. Биологическое значение митоза — точная передача наследственной информации, рост организма и регенерация тканей.`,
			want: "",
		},
		{
			name: "notes as a bulleted list",
			text: `Фазы митоза:
- профаза: хромосомы спирализуются, ядерная оболочка распадается;
- метафаза: хромосомы выстраиваются на экваторе клетки;
- анафаза: сестринские хроматиды расходятся к полюсам;
- телофаза: формируются ядерные оболочки, хромосомы деспирализуются.
Анафаза — самая короткая фаза митоза.`,
			want: "",
		},
		{
			name: "paragraph with numbers and a sentence ending in a number",
			text: `У человека в соматических клетках 46 хромосом, в половых клетках — 23. Синдром Дауна связан с наличием трёх копий 21-й хромосомы. Интерфаза занимает около 90 % времени клеточного цикла, а сам митоз длится от 30 до 60 минут.
Число хромосом у шимпанзе равно 48`,
			want: "",
		},
		{
			name: "text about a university is not a title page",
			text: `Первые университеты возникли в Европе в XI–XII веках. Студент средневекового университета сначала учился на факультете свободных искусств, а затем мог перейти на один из старших факультетов. Кафедра изначально означала место преподавателя в аудитории, а институт как форма организации появился значительно позже.`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := junkReason(tt.text); got != tt.want {
				t.Errorf("junkReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJunkFilterOnKonspekt(t *testing.T) {
	text := normalizeText(loadKonspekt(t))
	var kept, dropped []string
	for _, c := range splitChunks(text, DefaultChunkSize, DefaultChunkOverlap) {
		if reason := junkReason(c.Text); reason != "" {
			dropped = append(dropped, reason)
		} else {
			kept = append(kept, c.Text)
		}
	}

	for _, want := range []string{junkTitlePage, junkTOC, junkBibliography} {
		found := false
		for _, r := range dropped {
			found = found || r == want
		}
		if !found {
			t.Errorf("%s was not filtered, dropped: %v", want, dropped)
		}
	}
	// Every section of the lecture must reach the model.
	all := strings.Join(kept, "\n")
	for _, s := range []string{"Клетка — основная", "Интерфаза делится", "Митоз условно делят", "Мейоз — деление", "синдром Дауна"} {
		if !strings.Contains(all, s) {
			t.Errorf("section with %q was filtered out", s)
		}
	}
}
