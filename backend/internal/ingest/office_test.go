package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

// zipOf builds an Office file from its parts.
func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const docxBody = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Клеточный цикл</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Митоз — </w:t></w:r><w:r><w:t>непрямое деление.</w:t></w:r></w:p>
<w:tbl>
<w:tr><w:tc><w:p><w:r><w:t>daunting</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>пугающий</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>seamless</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>непрерывный</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
<w:p><w:r><w:instrText>PAGE</w:instrText></w:r></w:p>
</w:body></w:document>`

func TestExtractWordDocument(t *testing.T) {
	data := zipOf(t, map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": docxBody})
	if kind := DetectKind(data); kind != KindDOCX {
		t.Fatalf("DetectKind = %q, want docx", kind)
	}
	ex := newExtractor(t, &fakePDF{})
	res, err := ex.Extract(context.Background(), []File{{Name: "lecture.docx", Data: data}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	want := "Клеточный цикл\nМитоз — непрямое деление.\ndaunting | пугающий\nseamless | непрерывный"
	if res.Text != want {
		t.Errorf("text = %q\nwant %q", res.Text, want)
	}
	if len(res.Pages) != 1 || res.Pages[0].Source != SourceText {
		t.Errorf("pages = %+v, want one text page", res.Pages)
	}
}

func TestExtractPresentation(t *testing.T) {
	slide := func(lines ...string) string {
		var b strings.Builder
		b.WriteString(`<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree>`)
		for _, l := range lines {
			b.WriteString(`<p:sp><p:txBody><a:p><a:r><a:t>` + l + `</a:t></a:r></a:p></p:txBody></p:sp>`)
		}
		b.WriteString(`</p:spTree></p:cSld></p:sld>`)
		return b.String()
	}
	data := zipOf(t, map[string]string{
		"[Content_Types].xml":    "<Types/>",
		"ppt/presentation.xml":   "<p:presentation/>",
		"ppt/slides/slide10.xml": slide("Мейоз", "Редукционное деление."),
		"ppt/slides/slide2.xml":  slide("Митоз", "Непрямое деление."),
	})
	if kind := DetectKind(data); kind != KindPPTX {
		t.Fatalf("DetectKind = %q, want pptx", kind)
	}
	ex := newExtractor(t, &fakePDF{})
	res, err := ex.Extract(context.Background(), []File{{Name: "lecture.pptx", Data: data}})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if want := "Митоз\nНепрямое деление.\nМейоз\nРедукционное деление."; res.Text != want {
		t.Errorf("text = %q, want the slides in order %q", res.Text, want)
	}
	if len(res.Pages) != 2 {
		t.Errorf("%d pages, want one per slide", len(res.Pages))
	}
}

func TestExtractRejectsOtherArchives(t *testing.T) {
	data := zipOf(t, map[string]string{"photos/1.jpg": "x"})
	ex := newExtractor(t, &fakePDF{})
	_, err := ex.Extract(context.Background(), []File{{Name: "archive.zip", Data: data}})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Extract() error = %v, want unsupported", err)
	}
}
