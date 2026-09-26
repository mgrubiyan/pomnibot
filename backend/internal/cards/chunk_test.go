package cards

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func loadKonspekt(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/konspekt.txt")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return string(raw)
}

// checkChunks verifies the invariants every split must keep.
func checkChunks(t *testing.T, text string, chunks []chunk, size int) {
	t.Helper()
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk %d: Index = %d", i, c.Index)
		}
		if c.Start < 0 || c.End > len(text) || c.Start >= c.End {
			t.Fatalf("chunk %d: bad offsets [%d:%d] for text of %d bytes", i, c.Start, c.End, len(text))
		}
		if text[c.Start:c.End] != c.Text {
			t.Errorf("chunk %d: Text differs from text[Start:End]", i)
		}
		if n := utf8.RuneCountInString(c.Text); n > size {
			t.Errorf("chunk %d: %d characters, limit %d", i, n, size)
		}
		if strings.TrimSpace(c.Text) != c.Text {
			t.Errorf("chunk %d: starts or ends with whitespace: %q", i, c.Text)
		}
		if i == 0 {
			continue
		}
		prev := chunks[i-1]
		if c.Start <= prev.Start || c.End <= prev.End {
			t.Errorf("chunk %d [%d:%d] does not move past chunk %d [%d:%d]", i, c.Start, c.End, i-1, prev.Start, prev.End)
		}
		if c.Start > prev.End && strings.TrimSpace(text[prev.End:c.Start]) != "" {
			t.Errorf("text between chunks %d and %d is lost: %q", i-1, i, text[prev.End:c.Start])
		}
	}
	if len(chunks) > 0 {
		if strings.TrimSpace(text[:chunks[0].Start]) != "" || strings.TrimSpace(text[chunks[len(chunks)-1].End:]) != "" {
			t.Error("text before the first or after the last chunk is lost")
		}
	}
}

func TestSplitChunksShortText(t *testing.T) {
	text := "Митоз — непрямое деление соматических клеток.\n\nМейоз уменьшает число хромосом вдвое."
	chunks := splitChunks(text, 2500, 150)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Text != text || chunks[0].Start != 0 || chunks[0].End != len(text) {
		t.Errorf("chunk = %+v, want the whole text", chunks[0])
	}
	if got := splitChunks("  \n ", 2500, 150); got != nil {
		t.Errorf("blank text: got %d chunks, want none", len(got))
	}
}

func TestSplitChunksLongParagraph(t *testing.T) {
	sentence := "Клеточный цикл регулируется циклинами и циклинзависимыми киназами в контрольных точках. "
	text := strings.TrimSpace(strings.Repeat(sentence, 40)) // ~3600 characters, one line
	const size, overlap = 1000, 150

	chunks := splitChunks(text, size, overlap)
	if len(chunks) < 4 {
		t.Fatalf("got %d chunks, want at least 4", len(chunks))
	}
	checkChunks(t, text, chunks, size)
	for i, c := range chunks {
		if !strings.HasPrefix(c.Text, "Клеточный цикл") {
			t.Errorf("chunk %d does not start at a sentence: %q", i, c.Text[:40])
		}
		if !strings.HasSuffix(c.Text, ".") {
			t.Errorf("chunk %d does not end at a sentence", i)
		}
		if i > 0 {
			shared := chunks[i-1].End - c.Start
			if shared <= 0 {
				t.Errorf("chunk %d has no overlap with the previous one", i)
			}
			if n := utf8.RuneCountInString(text[c.Start:chunks[i-1].End]); n > overlap {
				t.Errorf("chunk %d overlaps by %d characters, limit %d", i, n, overlap)
			}
		}
	}
}

func TestSplitChunksHugeWord(t *testing.T) {
	text := "Ссылка: " + strings.Repeat("ж", 700) + " конец."
	chunks := splitChunks(text, 300, 0)
	checkChunks(t, text, chunks, 300)
	if len(chunks) < 3 {
		t.Errorf("got %d chunks, want the word cut into at least 3", len(chunks))
	}
}

func TestSplitChunksKonspekt(t *testing.T) {
	text := normalizeText(loadKonspekt(t))
	chunks := splitChunks(text, DefaultChunkSize, DefaultChunkOverlap)
	checkChunks(t, text, chunks, DefaultChunkSize)

	// The contents and the bibliography get fragments of their own, so the
	// filter drops them without the text next to them.
	var toc, bib *chunk
	for i := range chunks {
		c := &chunks[i]
		if strings.Contains(c.Text, "Содержание") {
			toc = c
		}
		if strings.Contains(c.Text, "Список литературы\n1.") {
			bib = c
		}
	}
	if toc == nil || strings.Contains(toc.Text, "Клетка — основная") {
		t.Errorf("contents share a fragment with the introduction: %+v", toc)
	}
	if bib == nil || strings.Contains(bib.Text, "синдром Дауна") {
		t.Errorf("bibliography shares a fragment with the last section: %+v", bib)
	}
}
