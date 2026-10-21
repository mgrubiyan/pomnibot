package ingest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
)

// buildDocument retains raw page readings, including pages with no usable text.
func buildDocument(res Result) material.Document {
	d := material.Document{Version: material.Version}
	for i, p := range res.Pages {
		end := len(res.Text)
		if i+1 < len(res.Pages) {
			end = res.Pages[i+1].Start
		}
		text := strings.TrimSpace(res.Text[p.Start:end])
		d.Pages = append(d.Pages, material.Page{ImageMime: p.ImageMime, Image: p.Image, Number: p.Number, File: p.File, OriginalPage: p.Page, TextLayer: p.TextLayer, Reading: text, Observations: p.Observations, Issues: p.Issues})
		blocks := p.Blocks
		if len(blocks) == 0 && text != "" {
			// Blank-line boundaries preserve paragraphs, code and formula groups. No
			// heuristic guesses about a missing diagram or column mapping are made.
			for _, part := range strings.Split(text, "\n\n") {
				if strings.TrimSpace(part) != "" {
					blocks = append(blocks, material.Block{Kind: "text", Text: strings.TrimSpace(part)})
				}
			}
		}
		for j, b := range blocks {
			b.ID, b.Page, b.Order = fmt.Sprintf("p%d-b%d", p.Number, j+1), p.Number, j
			if len(b.Issues) == 0 {
				b.Issues = append([]string(nil), p.Issues...)
			}
			if b.Observations == "" && j == 0 {
				b.Observations = p.Observations
			}
			d.Blocks = append(d.Blocks, b)
		}
	}
	data, _ := json.Marshal(d)
	d.Digest = material.Digest(data)
	return d
}

// ProcessingIdentity changes with both schema and reading instructions.
func ProcessingIdentity() string {
	return material.Digest([]byte(material.Version + fullPageSystem + string(pageSchema)))
}
