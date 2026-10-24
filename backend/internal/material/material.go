// Package material holds source readings, independently of a provider or card format.
package material

import (
	"crypto/sha256"
	"fmt"
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

// Evidence cites one continuous quotation from a source block.
type Evidence struct {
	BlockID string `json:"blockId"`
	Quote   string `json:"quote"`
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

// ValidRegion checks normalized image coordinates.
func ValidRegion(r *Region) bool {
	return r == nil || r[0] >= 0 && r[1] >= 0 && r[2] <= 1 && r[3] <= 1 && r[0] < r[2] && r[1] < r[3]
}
