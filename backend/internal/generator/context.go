package generator

import (
	"regexp"
	"slices"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
)

var formalNames = regexp.MustCompile(`\b[A-Z]{2,}(?:[ \t]+[A-Z]{2,})+\b`)

// relatedCode preserves source examples that mention a formal operation from
// another topic. This adds navigation context, not new claims or merged quotes.
// Only literal two-word names in source code and source text are matched.
func relatedCode(p *Plan, d material.Document) {
	for i := range p.Topics {
		t := &p.Topics[i]
		var source strings.Builder
		for _, id := range t.BlockIDs {
			if b, ok := d.Block(id); ok {
				source.WriteString(b.Text)
				source.WriteByte('\n')
			}
		}
		names := formalPairs(source.String())
		for _, b := range d.Blocks {
			if b.Kind != "code" || slices.Contains(t.BlockIDs, b.ID) {
				continue
			}
			for name := range formalPairs(b.Text) {
				if names[name] && !slices.Contains(t.RelatedBlockIDs, b.ID) {
					t.RelatedBlockIDs = append(t.RelatedBlockIDs, b.ID)
					break
				}
			}
		}
	}
}
func formalPairs(text string) map[string]bool {
	out := map[string]bool{}
	for _, run := range formalNames.FindAllString(text, -1) {
		words := strings.Fields(run)
		for i := 0; i+1 < len(words); i++ {
			out[words[i]+" "+words[i+1]] = true
		}
	}
	return out
}
func topicBlockIDs(t Topic) []string {
	out := slices.Clone(t.BlockIDs)
	for _, id := range t.RelatedBlockIDs {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
