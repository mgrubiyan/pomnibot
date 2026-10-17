package generator

import "testing"

const trigFragment = `Значения тригонометрических функций для некоторых углов.
sin 30° = 1/2, sin 45° = √2/2, sin 60° = √3/2.
При возрастании угла от 0° до 90° синус возрастает, а косинус убывает.
Угол между касательной и хордой измеряется половиной дуги, заключённой внутри угла.`

func TestCleanExplanation(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{
			name: "lead-in naming the notes is cut off",
			in:   "В конспекте указано, что синус 45° равен √2/2.",
			want: "Синус 45° равен √2/2.",
		},
		{
			name: "lead-in with an adverb",
			in:   "Во фрагменте чётко сказано, что угол между касательной и хордой измеряется половиной дуги.",
			want: "Угол между касательной и хордой измеряется половиной дуги.",
		},
		{
			name: "according to the notes",
			in:   "Согласно конспекту, при возрастании угла синус возрастает.",
			want: "При возрастании угла синус возрастает.",
		},
		{
			name: "a sentence about the notes alone is dropped",
			in:   "Синус возрастает при возрастании угла. Конкретная формулировка приведена в конспекте.",
			want: "Синус возрастает при возрастании угла.",
		},
		{
			name: "a sentence from outside the notes is dropped",
			in:   "Синус 30° равен 1/2. Это одно из базовых значений, широко используемых инженерами для расчётов.",
			want: "Синус 30° равен 1/2.",
		},
		{
			name: "a trailing clause naming where it is written",
			in:   "Синус 30° равен 1/2, что указано в таблице значений тригонометрических функций.",
			want: "Синус 30° равен 1/2.",
		},
		{
			name: "a trailing clause with как",
			in:   "При возрастании угла синус возрастает, как сказано выше.",
			want: "При возрастании угла синус возрастает.",
		},
		{
			name: "the notes as the subject",
			in:   "Конспект утверждает, что угол между касательной и хордой измеряется половиной дуги.",
			want: "Угол между касательной и хордой измеряется половиной дуги.",
		},
		{
			name: "a trailing clause with согласно",
			in:   "При возрастании угла синус возрастает, согласно определению в конспекте.",
			want: "При возрастании угла синус возрастает.",
		},
		{
			name: "a lead-in naming a table of the notes",
			in:   "В таблице значений указано, что sin 30° = 1/2.",
			want: "sin 30° = 1/2.",
		},
		{
			name: "a sentence about where the values are given is dropped",
			in:   "sin 30° = 1/2. В начале раздела указаны именно эти значения.",
			want: "sin 30° = 1/2.",
		},
		{
			name: "a formula without words stays",
			in:   "sin 60° = √3/2.",
			want: "sin 60° = √3/2.",
		},
		{
			name: "nothing left",
			in:   "Это фундаментальное понятие современной математики.",
			want: "",
		},
		{
			name: "a supported explanation is kept as is",
			in:   "При возрастании угла синус возрастает, а косинус убывает. Поэтому синус 60° больше синуса 30°.",
			want: "При возрастании угла синус возрастает, а косинус убывает. Поэтому синус 60° больше синуса 30°.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanExplanation(tc.in, trigFragment); got != tc.want {
				t.Errorf("cleanExplanation(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// Math notes write sin and cos where explanations say «синус» and
// «косинус»: the same words for the check.
func TestCleanExplanationReadsMathAbbreviations(t *testing.T) {
	fragment := "Тригонометрические функции тупого угла: cos(180° − α) = −cos α, sin(180° − α) = sin α. tg 45° = 1."
	for _, in := range []string{
		"Косинус тупого угла меняет знак, а синус тупого угла нет.",
		"Тангенс угла 45° равен 1.",
	} {
		if got := cleanExplanation(in, fragment); got != in {
			t.Errorf("cleanExplanation(%q) = %q, want it kept", in, got)
		}
	}
}
