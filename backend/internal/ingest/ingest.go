// Package ingest extracts the text of lecture notes from what a student
// sends: text files, PDFs and photos of pages. Several files are pages of one
// set of notes, in the order given.
//
// A PDF page with a text layer is read directly; a scanned one and a photo go
// to OCR, decided page by page. The text is returned as found:
// normalization belongs to the card generator, and nothing here rewrites or
// "improves" the text. Cards quote the student's notes, so a bad reading must
// be visible to the student, who gets a warning to retake the photo.
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
	NeedsOCR bool   `json:"needsOcr"` // a photo, or a PDF page with almost no text layer
	Poor     bool   `json:"poor"`     // recognized badly: the student should retake it
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

// OCR models: handwritten reads a mix of handwriting and print, the usual
// photo of notes; page is for a scan of a printed page.
const (
	ModelPhoto = "handwritten"
	ModelScan  = "page"
)

// OCR recognizes text in an image or a PDF. The Extractor keeps requests
// within the service limits.
type OCR interface {
	Recognize(ctx context.Context, req OCRRequest) ([]OCRPage, error)
}

// OCRRequest is one file to recognize.
type OCRRequest struct {
	Data     []byte
	MimeType string // image/jpeg, image/png or application/pdf
	Model    string
	Pages    int // pages in a PDF, 1 for an image
}

// OCRPage is the text of one page. Confidence is the mean over the page when
// the service reports it.
type OCRPage struct {
	Text          string
	Confidence    float64
	HasConfidence bool
}

// Options configure an Extractor.
type Options struct {
	MinPageChars int     // default DefaultMinPageChars
	PDF          PDFText // nil: pdftotext from PATH, checked in NewExtractor
	DisablePDF   bool    // no pdftotext needed; PDFs are rejected
	OCRModel     string  // overrides the choice of ModelPhoto or ModelScan
}

// Errors, wrapped in *Error.
var (
	ErrUnsupported  = errors.New("unsupported file type")
	ErrNoPDF        = errors.New("PDF support disabled")
	ErrNoOCR        = errors.New("OCR not configured")
	ErrTooLarge     = errors.New("file over the OCR size limit")
	ErrTooManyPages = errors.New("PDF over the OCR page limit")
	ErrOCR          = errors.New("OCR failed")
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
	ocr  OCR
	lim  limits
}

// NewExtractor returns an Extractor. ocr may be nil: photos are then
// rejected and scanned pages only produce a warning. Unless a PDFText is
// given or PDFs are disabled, NewExtractor checks for pdftotext right away,
// so a missing binary is found at start rather than on the first PDF.
func NewExtractor(ocr OCR, opts Options) (*Extractor, error) {
	if opts.MinPageChars <= 0 {
		opts.MinPageChars = DefaultMinPageChars
	}
	e := &Extractor{opts: opts, pdf: opts.PDF, ocr: ocr, lim: serviceLimits}
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
			text, err = e.readImage(ctx, &res, i, f)
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

// readPDF takes the text layer page by page. Pages with almost none go to
// OCR, and only their text is taken from it: a PDF of typed and photographed
// pages gives a mixed result. Pages of one PDF are joined with a single line
// break, so a sentence cut by a page break can be glued back by the
// generator's normalization.
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

	first := len(res.Pages)
	var scanned []int
	for i, text := range pages {
		info := PageInfo{
			Number: first + i + 1,
			File:   file,
			Page:   i + 1,
			Source: SourceText,
			Chars:  countChars(text),
		}
		if info.Chars < e.opts.MinPageChars {
			info.NeedsOCR = true
			scanned = append(scanned, i)
		}
		res.Pages = append(res.Pages, info)
	}

	if len(scanned) > 0 {
		if e.ocr == nil {
			for _, i := range scanned {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"Страница %d похожа на скан: в ней почти нет текста, а распознавание не настроено.", first+i+1))
			}
		} else if err := e.recognizePDF(ctx, res, f, pages, scanned, first); err != nil {
			return "", err
		}
	}

	var texts []string
	for _, text := range pages {
		if t := strings.TrimSpace(text); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// recognizePDF sends the whole PDF to OCR (the service takes a PDF, not a
// page of one) and replaces the text of the scanned pages.
func (e *Extractor) recognizePDF(ctx context.Context, res *Result, f File, pages []string, scanned []int, first int) error {
	switch {
	case len(pages) > e.lim.pages:
		return &Error{File: f.Name, Err: ErrTooManyPages, Message: fmt.Sprintf(
			"В PDF %d страниц, а распознать можно не больше %d. Разбейте файл на части.", len(pages), e.lim.pages)}
	case len(f.Data) > e.lim.bytes:
		return &Error{File: f.Name, Err: ErrTooLarge, Message: fmt.Sprintf(
			"PDF весит %.1f МБ, а распознать можно файл до %d МБ. Сожмите его или разбейте на части.",
			float64(len(f.Data))/1e6, e.lim.bytes/1_000_000)}
	}
	got, err := e.ocr.Recognize(ctx, OCRRequest{
		Data:     f.Data,
		MimeType: "application/pdf",
		Model:    e.model(ModelScan),
		Pages:    len(pages),
	})
	if err != nil {
		return e.ocrError(ctx, f, err)
	}
	for _, i := range scanned {
		info := &res.Pages[first+i]
		if i >= len(got) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Страница %d не распознана, попробуйте переснять.", info.Number))
			continue
		}
		pages[i] = got[i].Text
		e.markOCR(res, info, got[i])
	}
	return nil
}

// readImage recognizes a photo of a page, shrunk to the service limits first
// if needed.
func (e *Extractor) readImage(ctx context.Context, res *Result, file int, f File) (string, error) {
	if e.ocr == nil {
		return "", &Error{File: f.Name, Err: ErrNoOCR,
			Message: "Распознавание фото не настроено: пришлите текст или PDF."}
	}
	data, mime, _, err := fitImage(f.Data, e.lim)
	if err != nil {
		return "", &Error{File: f.Name, Err: ErrUnsupported,
			Message: "Не удалось открыть фото: пришлите JPEG или PNG."}
	}
	got, err := e.ocr.Recognize(ctx, OCRRequest{Data: data, MimeType: mime, Model: e.model(ModelPhoto), Pages: 1})
	if err != nil {
		return "", e.ocrError(ctx, f, err)
	}
	var page OCRPage
	if len(got) > 0 {
		page = got[0]
	}
	info := PageInfo{Number: len(res.Pages) + 1, File: file, Page: 1, NeedsOCR: true}
	res.Pages = append(res.Pages, info)
	e.markOCR(res, &res.Pages[len(res.Pages)-1], page)
	return strings.TrimSpace(page.Text), nil
}

func (e *Extractor) markOCR(res *Result, info *PageInfo, page OCRPage) {
	info.Source = SourceOCR
	info.Chars = countChars(page.Text)
	if poorRecognition(page) {
		info.Poor = true
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"Страница %d распознана плохо, попробуйте переснять.", info.Number))
	}
}

func (e *Extractor) model(auto string) string {
	if e.opts.OCRModel != "" {
		return e.opts.OCRModel
	}
	return auto
}

func (e *Extractor) ocrError(ctx context.Context, f File, err error) error {
	if ctx.Err() != nil {
		return err
	}
	return &Error{File: f.Name, Err: fmt.Errorf("%w: %w", ErrOCR, err),
		Message: "Не удалось распознать текст, попробуйте ещё раз позже."}
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
