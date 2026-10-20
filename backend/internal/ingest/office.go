package ingest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Office documents: a Word file or a PowerPoint deck is a zip of XML parts.
// The text is read with the standard library, paragraph by paragraph; a
// table row of Word becomes «cell | cell», so a vocabulary kept in a table
// reads as one.

// maxOfficePart caps one unpacked XML part: a zip bomb stops here.
const maxOfficePart = 32 << 20

var reSlide = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// officeKind tells a Word document or a PowerPoint deck from another zip.
func officeKind(data []byte) Kind {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return KindUnsupported
	}
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return KindDOCX
		case reSlide.MatchString(f.Name):
			return KindPPTX
		}
	}
	return KindUnsupported
}

// docxText returns the text of a Word document.
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			return partText(f, "p", "t")
		}
	}
	return "", errors.New("no word/document.xml")
}

// pptxSlides returns the text of each slide of a deck, in slide order.
func pptxSlides(data []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	type slide struct {
		n int
		f *zip.File
	}
	var slides []slide
	for _, f := range zr.File {
		if m := reSlide.FindStringSubmatch(f.Name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, slide{n, f})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	texts := make([]string, 0, len(slides))
	for _, s := range slides {
		text, err := partText(s.f, "p", "t")
		if err != nil {
			return nil, err
		}
		texts = append(texts, text)
	}
	return texts, nil
}

// partText reads the text runs of an XML part: a paragraph element ends a
// line, a table cell ends with « | » and a row with a line.
func partText(f *zip.File, para, run string) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(io.LimitReader(rc, maxOfficePart))
	var b, line strings.Builder
	var cells []string
	inRun, inCell := false, false
	endLine := func() {
		if s := strings.TrimSpace(line.String()); s != "" {
			b.WriteString(s)
			b.WriteByte('\n')
		}
		line.Reset()
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case run:
				inRun = true
			case "tab":
				line.WriteByte('\t')
			case "br", "cr":
				if inCell {
					line.WriteByte(' ')
				} else {
					endLine()
				}
			case "tc":
				inCell = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case run:
				inRun = false
			case para:
				if inCell {
					line.WriteByte(' ')
				} else {
					endLine()
				}
			case "tc":
				cells = append(cells, strings.TrimSpace(line.String()))
				line.Reset()
				inCell = false
			case "tr":
				line.WriteString(strings.Join(cells, " | "))
				cells = cells[:0]
				endLine()
			}
		case xml.CharData:
			if inRun {
				line.Write(t)
			}
		}
	}
	endLine()
	return strings.TrimSpace(b.String()), nil
}
