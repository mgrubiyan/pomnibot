package notes

import (
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
)

// Phone cameras and screenshots name files by date or number: such a name
// says nothing about the notes.
func TestIsGenericFilename(t *testing.T) {
	for name, want := range map[string]bool{
		"file_12":                        true,
		"file_12345.pdf":                 true,
		"photo_123.jpg":                  true,
		"IMG_1489.HEIC":                  true,
		"photo_2026-10-17 19.34.40.jpeg": true,
		"Снимок экрана 2026-10-17 в 21.18.32.png": true,
		"Screenshot 2026-10-17 at 21.18.32.png":   true,
		"scan0001.pdf":                            true,
		"Лекция 3. Клеточный цикл.pdf":            false,
		"konspekt.txt":                            false,
	} {
		if got := IsGenericFilename(name); got != want {
			t.Errorf("IsGenericFilename(%q) = %v, want %v", name, got, want)
		}
	}
}

// A file name becomes a title with spaces, not underscores.
func TestResolveSetTitleFromFilename(t *testing.T) {
	for name, want := range map[string]string{
		"лекция_биология.pdf": "лекция биология",
		"Этапы обработки кода программы на C++_ пространства имён и локали.pdf": "Этапы обработки кода программы на C++ пространства имён и локали",
		"__Лекция__4__.docx":           "Лекция 4",
		"Лекция 3. Клеточный цикл.pdf": "Лекция 3. Клеточный цикл",
		// A title or name with no extension keeps everything after its dots.
		"Лекция 3. Клеточный цикл": "Лекция 3. Клеточный цикл",
		"Версия 1.2": "Версия 1.2",
	} {
		if got := resolveSetTitle("", name); got != want {
			t.Errorf("resolveSetTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

// A generic name stays generic once its underscores turn into spaces,
// so the set is still renamed after its topics.
func TestIsGenericTitleAfterSpaces(t *testing.T) {
	for _, name := range []string{"file_12345.pdf", "IMG_1489.HEIC", "DSC_0042.jpg"} {
		if title := resolveSetTitle("", name); !isGenericTitle(title) {
			t.Errorf("isGenericTitle(resolveSetTitle(%q) = %q) = false, want true", name, title)
		}
	}
}

// A set made from photos with no caption is named after its commonest topic,
// not «Новый конспект (17.10 23:55)».
func TestTitleFromTopics(t *testing.T) {
	facts := []generator.Fact{{Name: "Фазы митоза"}, {Name: ""}, {Name: "Клеточный цикл"}, {Name: "Фазы митоза"}}
	if got := titleFromFacts(facts); got != "Фазы митоза" {
		t.Errorf("titleFromFacts = %q, want the commonest topic", got)
	}
	if got := titleFromFacts([]generator.Fact{{Name: ""}}); got != "" {
		t.Errorf("titleFromFacts = %q, want none without topics", got)
	}
}
