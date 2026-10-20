package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PDFText extracts the text layer of a PDF, one string per page, in page
// order. A page without a text layer (a scan, a photo) comes back empty or
// nearly so.
type PDFText interface {
	Pages(ctx context.Context, pdf []byte) ([]string, error)
}

// ErrNoPDFToText means pdftotext (poppler-utils) is not installed.
var ErrNoPDFToText = errors.New("ingest: pdftotext not found; install poppler-utils")

// pdftotext runs poppler's pdftotext.
type pdftotext struct {
	path string
}

func newPDFToText(name string) (*pdftotext, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPDFToText, err)
	}
	return &pdftotext{path: path}, nil
}

// Pages writes the PDF to a temporary file (pdftotext needs a seekable
// input) and splits the output on form feeds, which pdftotext puts after
// every page.
func (p *pdftotext) Pages(ctx context.Context, pdf []byte) ([]string, error) {
	f, err := os.CreateTemp("", "ingest-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("ingest: temp file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(pdf); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("ingest: write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("ingest: close temp file: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, p.path, "-enc", "UTF-8", "-eol", "unix", f.Name(), "-")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ingest: pdftotext: %w", ctx.Err())
		}
		// Exit codes: 1 cannot open the file, 3 copying text not permitted.
		return nil, fmt.Errorf("ingest: PDF damaged or password-protected: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return splitPages(stdout.String()), nil
}

// splitPages cuts pdftotext output into pages. Every page ends with a form
// feed, so the piece after the last one is not a page.
func splitPages(out string) []string {
	pages := strings.Split(out, "\f")
	if n := len(pages); n > 0 && strings.TrimSpace(pages[n-1]) == "" {
		pages = pages[:n-1]
	}
	return pages
}
