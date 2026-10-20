package ingest

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Recognition quality. The OCR service reports no confidence scores, so a
// page is judged by what came out: garbage characters and lines broken into
// crumbs are what a blurred, skewed or dark photo turns into. Nothing here
// tries to repair the text: cards quote the notes, and a bad reading must be
// seen by the student, who can retake the photo.
const (
	// minConfidence: below this mean confidence (when the service reports
	// one) a page is poor.
	minConfidence = 0.6
	// maxGarbageShare: share of non-space characters that are neither
	// letters, digits, punctuation nor common math signs.
	maxGarbageShare = 0.1
	// maxShortLineShare: share of non-empty lines of at most shortLine
	// characters.
	maxShortLineShare = 0.4
	shortLine         = 2
	minLinesForShort  = 5
	// minOCRChars: a recognized page with less text than this is poor: the
	// photo is blank, blurred or not of a page.
	minOCRChars = 20
)

// mathSigns are symbols that belong in notes, not garbage.
const mathSigns = "=+-−<>≤≥×÷±%°№√∑∫∞≈≠^~/|'\""

// poorRecognition reports whether an OCR page looks badly recognized.
func poorRecognition(p OCRPage) bool {
	if p.HasConfidence {
		return p.Confidence < minConfidence
	}
	var chars, garbage int
	for _, r := range p.Text {
		if unicode.IsSpace(r) {
			continue
		}
		chars++
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsPunct(r) && !strings.ContainsRune(mathSigns, r) {
			garbage++
		}
	}
	if chars < minOCRChars {
		return true
	}
	if float64(garbage)/float64(chars) > maxGarbageShare {
		return true
	}

	var lines, short int
	for line := range strings.SplitSeq(p.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines++
		if utf8.RuneCountInString(line) <= shortLine {
			short++
		}
	}
	return lines >= minLinesForShort && float64(short)/float64(lines) > maxShortLineShare
}
