// Package ingest extracts the text of lecture notes from what a student
// sends: text files and PDFs. Several files are pages of one set of notes, in
// the order given.
//
// The text is returned as found. Normalization (hyphenation, soft-wrapped
// lines) belongs to the card generator, and nothing here rewrites the text:
// cards quote the student's notes, so what the student sees must be what the
// notes say.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// File is one file as received, with whatever name the messenger gave it.
type File struct {
	Name string
	Data []byte
}

// Source tells where the text came from.
type Source string

// Sources of text.
const (
	SourceText  Source = "text"  // a text file or a PDF text layer
	SourceOCR   Source = "ocr"   // recognized from images or scans
	SourceMixed Source = "mixed" // some pages of each
)

// PageInfo describes one page of the result.
type PageInfo struct {
	Number   int    `json:"number"` // 1-based across all files, as the student sees them
	File     int    `json:"file"`   // index in the files passed to Extract
	Page     int    `json:"page"`   // 1-based within its file
	Source   Source `json:"source"`
	Chars    int    `json:"chars"`    // non-space characters of the page text
	NeedsOCR bool   `json:"needsOcr"` // a PDF page with almost no text layer: a scan
}

// Result is the extracted text. Warnings are for the student, in Russian: the
// confirmation screen shows them before cards are made.
type Result struct {
	Text     string     `json:"text"`
	Source   Source     `json:"source"`
	Pages    []PageInfo `json:"pages"`
	Warnings []string   `json:"warnings,omitempty"`
}

// DefaultMinPageChars is the text layer size, in non-space characters, below
// which a PDF page is taken for a scan. A typed page of notes has well over a
// thousand; a scan has none, or a page number and a header.
const DefaultMinPageChars = 100

// Options configure an Extractor.
type Options struct {
	MinPageChars int     // default DefaultMinPageChars
	PDF          PDFText // nil: pdftotext from PATH, checked in New
	DisablePDF   bool    // no pdftotext needed; PDFs are rejected
}

// Errors, wrapped in *Error.
var (
	ErrUnsupported = errors.New("unsupported file type")
	ErrNoPDF       = errors.New("PDF support disabled")
)

// Error is a problem with the student's files. Message is for the student.
type Error struct {
	File    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	return fmt.Sprintf("ingest: %s: %v", e.File, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Extractor turns files into text. It is safe for concurrent use.
type Extractor struct {
	opts Options
	pdf  PDFText
}

// New returns an Extractor. Unless a PDFText is given or PDFs are disabled,
// it checks for pdftotext right away, so a missing binary is found at start
// rather than on the first PDF.
func New(opts Options) (*Extractor, error) {
	if opts.MinPageChars <= 0 {
		opts.MinPageChars = DefaultMinPageChars
	}
	e := &Extractor{opts: opts, pdf: opts.PDF}
	if e.pdf == nil && !opts.DisablePDF {
		p, err := newPDFToText("pdftotext")
		if err != nil {
			return nil, err
		}
		e.pdf = p
	}
	return e, nil
}

// Extract reads files as pages of one set of notes, in the given order.
func (e *Extractor) Extract(ctx context.Context, files []File) (Result, error) {
	var res Result
	var parts []string
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		var text string
		var err error
		switch kind := DetectKind(f.Data); kind {
		case KindText:
			text = e.readText(&res, i, f)
		case KindPDF:
			text, err = e.readPDF(ctx, &res, i, f)
		case KindJPEG, KindPNG:
			err = &Error{File: f.Name, Err: ErrUnsupported,
				Message: "Фотографии пока не принимаются: пришлите текст или PDF."}
		default:
			err = &Error{File: f.Name, Err: ErrUnsupported,
				Message: "Формат не поддерживается: пришлите текст, PDF или фото страниц."}
		}
		if err != nil {
			return Result{}, err
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	res.Text = strings.Join(parts, "\n\n")
	res.Source = overallSource(res.Pages)
	return res, nil
}

func (e *Extractor) readText(res *Result, file int, f File) string {
	text, enc := decodeText(f.Data)
	if enc != "" {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("Файл «%s» в кодировке %s, текст перекодирован.", f.Name, enc))
	}
	res.Pages = append(res.Pages, PageInfo{
		Number: len(res.Pages) + 1,
		File:   file,
		Page:   1,
		Source: SourceText,
		Chars:  countChars(text),
	})
	return text
}

// readPDF takes the text layer page by page. Pages of one PDF are joined with
// a single line break, so a sentence cut by a page break can be glued back
// by the generator's normalization.
func (e *Extractor) readPDF(ctx context.Context, res *Result, file int, f File) (string, error) {
	if e.pdf == nil {
		return "", &Error{File: f.Name, Err: ErrNoPDF, Message: "PDF сейчас не принимаются."}
	}
	pages, err := e.pdf.Pages(ctx, f.Data)
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		return "", &Error{File: f.Name, Err: err,
			Message: "Не удалось прочитать PDF: файл повреждён или защищён паролем."}
	}
	var texts []string
	for i, text := range pages {
		info := PageInfo{
			Number: len(res.Pages) + 1,
			File:   file,
			Page:   i + 1,
			Source: SourceText,
			Chars:  countChars(text),
		}
		if info.Chars < e.opts.MinPageChars {
			info.NeedsOCR = true
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"Страница %d похожа на скан: в ней почти нет текста, а распознавание сканов пока не подключено.", info.Number))
		}
		res.Pages = append(res.Pages, info)
		if strings.TrimSpace(text) != "" {
			texts = append(texts, strings.TrimSpace(text))
		}
	}
	return strings.Join(texts, "\n"), nil
}

func overallSource(pages []PageInfo) Source {
	src := SourceText
	for i, p := range pages {
		switch {
		case i == 0:
			src = p.Source
		case p.Source != src:
			return SourceMixed
		}
	}
	return src
}

func countChars(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
