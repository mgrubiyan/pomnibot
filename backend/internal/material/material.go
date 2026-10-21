// Package material holds source readings, independently of a provider or card format.
package material

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Version invalidates stored processing artifacts when reading semantics change.
const Version = "structured-reading-v2"

// Document preserves page order, separate original readings and derived blocks.
type Document struct {
	Version string   `json:"version"`
	Digest  string   `json:"digest"`
	Pages   []Page   `json:"pages"`
	Blocks  []Block  `json:"blocks"`
	Issues  []string `json:"issues,omitempty"`
}

// Page preserves the original text layer and the independent image reading.
type Page struct {
	ImageMime    string   `json:"imageMime,omitempty"`
	Number       int      `json:"number"`
	Image        []byte   `json:"image,omitempty"` // exact image seen by the reader; private artifact only
	File         int      `json:"file"`
	OriginalPage int      `json:"originalPage"`
	TextLayer    string   `json:"textLayer,omitempty"`
	Reading      string   `json:"reading"`
	Observations string   `json:"observations,omitempty"`
	Issues       []string `json:"issues,omitempty"`
}

// Region uses normalized image coordinates [left, top, right, bottom].
type Region [4]float64

// Block keeps transcription in Text, formatting in Normalized, and interpretation in Observations.
// Observations is model interpretation and can never satisfy a text quotation.
type Block struct {
	ID           string     `json:"id"`
	Page         int        `json:"page"`
	Order        int        `json:"order"`
	Kind         string     `json:"kind"` // heading, text, list, code, formula, table, diagram, image
	Text         string     `json:"text"`
	Normalized   string     `json:"normalized,omitempty"`
	Cells        [][]string `json:"cells,omitempty"`
	Region       *Region    `json:"region,omitempty"`
	Observations string     `json:"observations,omitempty"`
	Issues       []string   `json:"issues,omitempty"`
}

// Evidence spans remain separate, even when a claim needs distant pages.
// Row and Column are zero-based; cell coordinates apply to kind=cell.
type Evidence struct {
	BlockID     string  `json:"blockId"`
	Kind        string  `json:"kind"`
	Quote       string  `json:"quote"`
	Row         int     `json:"row"`
	Column      int     `json:"column"`
	Region      *Region `json:"region,omitempty"`
	Observation string  `json:"observation,omitempty"` // image claim, never a quotation
}

// Digest identifies content without exposing it.
func Digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// Block looks up an immutable source block.
func (d Document) Block(id string) (Block, bool) {
	for _, b := range d.Blocks {
		if b.ID == id {
			return b, true
		}
	}
	return Block{}, false
}

// ValidEvidence checks a text, table-cell or image-region anchor.
func (d Document) ValidEvidence(e Evidence) bool {
	if e.Kind != "" && e.Kind != "text" && e.Kind != "cell" && e.Kind != "image" {
		return false
	}
	b, ok := d.Block(e.BlockID)
	if !ok {
		return false
	}
	if e.Kind == "image" {
		if e.Quote != "" || e.Observation == "" || e.Region == nil || !ValidRegion(e.Region) {
			return false
		}
		if b.Region != nil && (e.Region[0] < b.Region[0] || e.Region[1] < b.Region[1] || e.Region[2] > b.Region[2] || e.Region[3] > b.Region[3]) {
			return false
		}
		for _, p := range d.Pages {
			if p.Number == b.Page && len(p.Image) > 0 {
				return true
			}
		}
		return false
	}
	if strings.TrimSpace(e.Quote) == "" {
		return false
	}
	if e.Kind == "cell" || len(b.Cells) > 0 && b.Text == "" {
		return e.Row >= 0 && e.Row < len(b.Cells) && e.Column >= 0 && e.Column < len(b.Cells[e.Row]) && strings.Contains(b.Cells[e.Row][e.Column], e.Quote)
	}
	// Visual descriptions require subsequent image review; they are never quotes.

	return strings.Contains(b.Text, e.Quote)
}

// Content includes explicit table coordinates without flattening column relations.
func (b Block) Content() string {
	var s strings.Builder
	s.WriteString(b.Text)
	for r, row := range b.Cells {
		for c, v := range row {
			fmt.Fprintf(&s, "\ncell[%d,%d]=%s", r, c, v)
		}
	}
	return s.String()
}

// ValidRegion checks normalized image coordinates.
func ValidRegion(r *Region) bool {
	return r == nil || r[0] >= 0 && r[1] >= 0 && r[2] <= 1 && r[3] <= 1 && r[0] < r[2] && r[1] < r[3]
}
