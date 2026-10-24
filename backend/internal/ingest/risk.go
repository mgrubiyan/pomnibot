package ingest

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// PDFVisualRisk is optional: a text extractor can identify pages whose layer
// cannot represent their content. Unknown inspection errs toward image reading.
type PDFVisualRisk interface {
	VisualPages(context.Context, []byte, []string) ([]bool, error)
}

func (p *pdftotext) VisualPages(ctx context.Context, data []byte, pages []string) ([]bool, error) {
	risk := make([]bool, len(pages))
	for i, text := range pages {
		risk[i] = layoutRisk(text)
	}
	f, err := os.CreateTemp("", "ingest-risk-*.pdf")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "pdfimages", "-list", f.Name()).Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		page, pe := strconv.Atoi(fields[0])
		w, we := strconv.Atoi(fields[3])
		h, he := strconv.Atoi(fields[4])
		// Text may be entirely inside a bitmap even on an otherwise rich text page.
		if pe == nil && we == nil && he == nil && page > 0 && page <= len(risk) && w >= 200 && h >= 80 {
			risk[page-1] = true
		}
	}
	return risk, nil
}
func layoutRisk(text string) bool {
	lines, short, columns := 0, 0, 0
	for _, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		lines++
		if len([]rune(s)) < 30 {
			short++
		}
		if strings.Contains(s, "   ") {
			columns++
		}
		if strings.ContainsAny(s, "∫∑√∂∀∃→←↔≤≥≠⊂∈^\\") {
			return true
		}
		for _, r := range s {
			if unicode.Is(unicode.Greek, r) {
				return true
			}
		}
	}
	// Many short positioned labels commonly indicate tables or vector diagrams.
	return columns >= 2 || lines >= 5 && short*2 > lines
}
