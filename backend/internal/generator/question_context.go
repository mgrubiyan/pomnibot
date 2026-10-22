package generator

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
)

// Preserve visible page labels as question context before either reviewer sees
// the card. Reading order may place a slide subtitle after its formulas. A
// caption with a parenthesized qualifier remains necessary source context even
// when OCR classified it as text. Never invent units or shorten source labels.
func bindCardContext(c cards.Card, d material.Document) (cards.Card, []string) {
	pages := map[int]bool{}
	for _, e := range c.Evidence {
		if b, ok := d.Block(e.BlockID); ok {
			pages[b.Page] = true
		}
	}
	var anchors []string
	for _, e := range c.Evidence {
		anchors = appendUnique(anchors, e.BlockID)
	}
	headingsFor := func(ids []string) []string {
		p := Plan{Topics: []Topic{{BlockIDs: slices.Clone(ids), Objectives: []Objective{{BlockIDs: slices.Clone(ids)}}}}}
		bindObjectiveScopes(&p, d)
		var headings []string
		for _, id := range p.Topics[0].Objectives[0].BlockIDs {
			if b, ok := d.Block(id); ok && b.Kind == "heading" {
				headings = appendUnique(headings, id)
			}
		}
		return headings
	}
	headings := headingsFor(anchors)
	firstBlock := map[int]string{}
	for _, b := range d.Blocks {
		if _, exists := firstBlock[b.Page]; !exists {
			firstBlock[b.Page] = b.ID
		}
	}
	var labels, ids []string
	for _, b := range d.Blocks {
		text := strings.TrimSpace(b.Text)
		caption := isContextCaption(b)
		if b.Page <= 0 || !pages[b.Page] || text == "" || utf8.RuneCountInString(text) > 320 || b.Kind != "heading" && !caption {
			continue
		}
		// Column and section headings do not necessarily scope a full-width
		// footer. Only the original page title is safe to add automatically.
		if b.Kind == "heading" && firstBlock[b.Page] != b.ID {
			continue
		}
		if caption {
			localHeadings := headingsFor([]string{b.ID})
			if len(localHeadings) > 0 && !slices.ContainsFunc(localHeadings, func(id string) bool { return slices.Contains(headings, id) }) {
				continue
			}
		}
		if questionContainsScope(c.Question, text) || namedIn(c.Answer, text) {
			continue
		}
		if !slices.Contains(labels, text) {
			labels = append(labels, text)
		}
		ids = appendUnique(ids, b.ID)
		c.Evidence = mergeEvidence(c.Evidence, []material.Evidence{{BlockID: b.ID, Kind: "text", Quote: b.Text}})
	}
	if len(labels) > 0 {
		c.Question = strings.Join(labels, ". ") + ".\n\n" + c.Question
	}
	return c, ids
}
