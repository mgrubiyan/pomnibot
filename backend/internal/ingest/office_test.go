package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"slices"
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

func TestPresentationPreservesInlineBreaksAndTabs(t *testing.T) {
	paragraphs := `<a:p><a:r><a:t>SELECT</a:t></a:r><a:tab/><a:r><a:t>id</a:t></a:r><a:br/><a:r><a:t>FROM</a:t><a:tab/><a:t>students;</a:t></a:r></a:p><a:p><a:r><a:t>Next paragraph</a:t></a:r></a:p>`
	want := "SELECT\tid\nFROM\tstudents;\nNext paragraph"
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "shape", body: `<p:spTree><p:sp><p:txBody>` + paragraphs + `</p:txBody></p:sp></p:spTree>`},
		{name: "table cell", body: `<p:spTree><p:graphicFrame><a:tbl><a:tr><a:tc><a:txBody>` + paragraphs + `</a:txBody></a:tc></a:tr></a:tbl></p:graphicFrame></p:spTree>`},
		{name: "fallback", body: paragraphs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := zipOf(t, map[string]string{
				"ppt/slides/slide1.xml": `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld>` + tc.body + `</p:cSld></p:sld>`,
			})
			res, err := newExtractor(t, &fakePDF{}).Extract(context.Background(), []File{{Name: "query.pptx", Data: data}})
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != want {
				t.Errorf("text = %q, want %q", res.Text, want)
			}
			if len(res.Document.Blocks) != 1 || res.Document.Blocks[0].Text != want {
				t.Fatalf("source block separators lost: %+v", res.Document.Blocks)
			}
			if tc.name == "table cell" && res.Document.Blocks[0].Cells[0][0] != want {
				t.Errorf("table cell = %q, want %q", res.Document.Blocks[0].Cells[0][0], want)
			}
		})
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

func TestPresentationPreservesTablesAndFlagsUnrenderedConnections(t *testing.T) {
	slide := `<sld><cSld><spTree><sp><txBody><p><r><t>Типы</t></r></p></txBody></sp><graphicFrame><tbl><tr><tc><p><r><t>Тип</t></r></p></tc><tc><p><r><t>Описание</t></r></p></tc></tr><tr><tc><p><r><t>INT</t></r></p></tc><tc><p><r><t>Целое</t></r></p></tc></tr></tbl></graphicFrame><cxnSp><nvCxnSpPr><cNvCxnSpPr><stCxn id="2"/><endCxn id="3"/></cNvCxnSpPr></nvCxnSpPr></cxnSp></spTree></cSld></sld>`
	data := zipOf(t, map[string]string{"ppt/slides/slide1.xml": slide})
	ex := newExtractor(t, &fakePDF{})
	res, err := ex.Extract(context.Background(), []File{{Name: "tables.pptx", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range res.Document.Blocks {
		if b.Kind == "table" {
			found = true
			if len(b.Cells) != 2 || b.Cells[1][1] != "Целое" {
				t.Fatalf("columns lost: %+v", b)
			}
		}
	}
	if !found || len(res.Document.Pages[0].Issues) == 0 {
		t.Fatal("table structure or connection uncertainty lost")
	}
}

func TestPresentationUsesDeclaredSlideOrder(t *testing.T) {
	for _, prefix := range []string{"", "/ppt/"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			data := zipOf(t, map[string]string{
				"ppt/presentation.xml":            `<presentation xmlns:r="r"><sldIdLst><sldId id="1" r:id="first"/><sldId id="2" r:id="second"/></sldIdLst></presentation>`,
				"ppt/_rels/presentation.xml.rels": fmt.Sprintf(`<Relationships><Relationship Id="first" Target="%sslides/slide7.xml"/><Relationship Id="second" Target="%sslides/slide2.xml"/></Relationships>`, prefix, prefix),
				"ppt/slides/slide2.xml":           `<sld><p><t>Второй слайд</t></p></sld>`,
				"ppt/slides/slide7.xml":           `<sld><p><t>Первый слайд</t></p></sld>`,
			})
			ex := newExtractor(t, &fakePDF{})
			res, err := ex.Extract(context.Background(), []File{{Name: "order.pptx", Data: data}})
			if err != nil || !strings.HasPrefix(res.Text, "Первый слайд") {
				t.Fatalf("declared order lost: %q %v", res.Text, err)
			}
		})
	}
}

func TestPresentationResolvesEmbeddedImageTargets(t *testing.T) {
	image := pngBytes(t, 4, 4)
	for _, target := range []string{"../media/image1.png", "/ppt/media/image1.png"} {
		t.Run(target, func(t *testing.T) {
			data := zipOf(t, map[string]string{
				"ppt/slides/slide1.xml":            `<sld xmlns:r="r"><cSld><spTree><pic><blipFill><blip r:embed="image"/></blipFill></pic></spTree></cSld></sld>`,
				"ppt/slides/_rels/slide1.xml.rels": fmt.Sprintf(`<Relationships><Relationship Id="image" Target="%s"/></Relationships>`, target),
				"ppt/media/image1.png":             string(image),
			})
			ocr := &fakeOCR{answer: func(int, OCRRequest) ([]OCRPage, error) {
				return []OCRPage{{Text: "Текст изображения"}}, nil
			}}
			res, err := newOCRExtractor(t, ocr, &fakePDF{}).Extract(context.Background(), []File{{Name: "image.pptx", Data: data}})
			if err != nil {
				t.Fatal(err)
			}
			if len(ocr.reqs) != 1 || !bytes.Equal(ocr.reqs[0].Data, image) {
				t.Fatalf("embedded image not passed to OCR: %+v", ocr.reqs)
			}
			if res.Text != "Текст изображения" || len(res.Document.Blocks) != 1 || res.Document.Blocks[0].Kind != "image" {
				t.Fatalf("embedded image content lost: %+v", res)
			}
		})
	}
}

func TestPresentationNamesOversizedEmbeddedImage(t *testing.T) {
	data := zipOf(t, map[string]string{
		"ppt/slides/slide1.xml":            `<sld xmlns:r="r"><cSld><spTree><sp><txBody><p><r><t>Митоз — непрямое деление соматических клеток.</t></r></p></txBody></sp><pic><blipFill><blip r:embed="image"/></blipFill></pic></spTree></cSld></sld>`,
		"ppt/slides/_rels/slide1.xml.rels": `<Relationships><Relationship Id="image" Target="../media/image1.png"/></Relationships>`,
		"ppt/media/image1.png":             string(bombPNG(overLimit, overLimit)),
	})
	ocr := &fakeOCR{}
	res, err := newOCRExtractor(t, ocr, &fakePDF{}).Extract(context.Background(), []File{{Name: "deck.pptx", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ocr.reqs) != 0 {
		t.Errorf("OCR called for an image over the pixel limit: %+v", ocr.reqs)
	}
	if len(res.Pages) != 1 || !slices.Contains(res.Pages[0].Issues, "Изображение внутри слайда слишком большое") {
		t.Errorf("pages = %+v, want the slide to name the image as too large", res.Pages)
	}
}
