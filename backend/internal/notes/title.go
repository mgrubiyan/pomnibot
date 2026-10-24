package notes

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
)

// genericFileRegex matches names cameras, messengers and screenshots give
// files, «IMG_1489.HEIC», «photo_2026-10-17 19.34.40.jpeg», «Снимок экрана …»:
// they say nothing about the notes.
// Separators match spaces too: a title made from a file name has its
// underscores turned into spaces and must still read as generic.
var genericFileRegex = regexp.MustCompile(`(?i)^(file[_ ]\d+(?:\.\w+)?|photo[_ -].*|img[_ -]?\d.*|image(?:[_ -]?\d+)?(?:\.\w+)?|scan[_ -]?\d.*|снимок экрана.*|screenshot.*|dsc[_ -]?\w*\d.*|изображение.*)$`)

// A file extension: letters first, short, no spaces. A dot elsewhere is part
// of the name — «Лекция 3. Клеточный цикл», «Версия 1.2».
var fileExtRegex = regexp.MustCompile(`^\.[\pL][\pL\d]{0,4}$`)

func trimFileExt(name string) string {
	if ext := filepath.Ext(name); fileExtRegex.MatchString(ext) {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// IsGenericFilename tells whether a file name says nothing about the notes,
// like the names cameras and screenshots give files.
func IsGenericFilename(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || genericFileRegex.MatchString(name)
}

func isGenericTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || genericFileRegex.MatchString(title) {
		return true
	}
	if strings.HasPrefix(title, "Новый конспект (") {
		return true
	}
	return false
}

// titleFromFacts names a set after its commonest fact name, the first on a tie,
// or returns "" when the facts have none.
func titleFromFacts(facts []generator.Fact) string {
	counts := map[string]int{}
	best := ""
	for _, f := range facts {
		if f.Name == "" {
			continue
		}
		counts[f.Name]++
		if counts[f.Name] > counts[best] {
			best = f.Name
		}
	}
	return best
}

func resolveSetTitle(caption, filename string) string {
	caption = strings.TrimSpace(caption)
	if caption != "" {
		lines := strings.Split(caption, "\n")
		title := strings.TrimSpace(lines[0])
		runes := []rune(title)
		if len(runes) > 80 {
			title = string(runes[:80]) + "..."
		}
		return title
	}
	if filename != "" && !IsGenericFilename(filename) {
		base := trimFileExt(filename)
		// «Лекция_4_ОРГ» reads as «Лекция 4 ОРГ»: the title is shown on
		// the home screen and under every quote.
		base = strings.Join(strings.Fields(strings.ReplaceAll(base, "_", " ")), " ")
		if base != "" {
			runes := []rune(base)
			if len(runes) > 80 {
				base = string(runes[:80]) + "..."
			}
			return base
		}
	}
	return fmt.Sprintf("Новый конспект (%s)", time.Now().Format("02.01 15:04"))
}
