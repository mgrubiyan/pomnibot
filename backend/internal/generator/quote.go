package generator

import (
	"strings"
	"unicode"
)

// sourceQuote returns an actual contiguous source span. Only whitespace and
// leading bullet glyphs may differ. It cannot skip conditions using ellipses,
// fix spelling, replace symbols or join disjoint sentences.
func sourceQuote(source, quote string) (string, bool) {
	fold := func(s string) (string, []int, []int) {
		var out strings.Builder
		var starts, ends []int
		pendingStart, pendingEnd := -1, -1
		lineStart := true
		for i, r := range s {
			end := i + len(string(r))
			if unicode.IsSpace(r) || (lineStart && strings.ContainsRune("•◦▪", r)) {
				if pendingStart < 0 {
					pendingStart = i
				}
				pendingEnd = end
				if r == '\n' {
					lineStart = true
				}
				continue
			}
			if pendingStart >= 0 && out.Len() > 0 {
				out.WriteByte(' ')
				starts = append(starts, pendingStart)
				ends = append(ends, pendingEnd)
			}
			pendingStart = -1
			lineStart = false
			n, _ := out.WriteRune(r)
			for range n {
				starts = append(starts, i)
				ends = append(ends, end)
			}
		}
		return out.String(), starts, ends
	}
	q, _, _ := fold(quote)
	if q == "" {
		return "", false
	}
	s, starts, ends := fold(source)
	i := strings.Index(s, q)
	if i < 0 {
		return "", false
	}
	return source[starts[i]:ends[i+len(q)-1]], true
}
