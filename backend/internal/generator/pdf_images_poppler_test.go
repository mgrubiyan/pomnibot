package generator

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os/exec"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// Actual PDF rendering covers embedded photos/scans and vector paths/text together.
func TestPDFCropsWithPopplerAndRotatedPages(t *testing.T) {
	for _, name := range []string{"pdfinfo", "pdftoppm"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " is unavailable")
		}
	}
	for _, rotation := range []int{0, 90, 180, 270} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			pdf := figurePDF(t, rotation)
			rendered, err := ingest.RenderPDFPage(t.Context(), pdf, 0)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(rendered))
			if err != nil {
				t.Fatal(err)
			}
			if rotation == 90 || rotation == 270 {
				if cfg.Width <= cfg.Height {
					t.Fatal("rotated page not upright")
				}
			} else if cfg.Width >= cfg.Height {
				t.Fatal("unexpected portrait geometry")
			}
			data, w, h, err := cropPDFImage(rendered, material.Region{.1, .1, .9, .9})
			if err != nil || w <= 0 || h <= 0 {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			colored, dark := 0, 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r > g+10000 && r > b+10000 {
						colored++
					}
					if r < 16000 && g < 16000 && b < 16000 {
						dark++
					}
				}
			}
			if colored < 100 || dark < 50 {
				t.Fatalf("lost photo/vector caption: colored=%d dark=%d", colored, dark)
			}
		})
	}
}

func TestPDFImageSourcesAcrossFiles(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 80))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	var seenFiles [][]byte
	p := providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
		if r.System == imageDiscoveryPrompt {
			return providers.Response{Content: []byte(`{"figures":[{"file":2,"page":1,"description":"scan"},{"file":1,"page":2,"description":"vector"}]}`)}, nil
		}
		if r.System == imageLocalizationPrompt {
			index := 0
			if len(seenFiles) == 2 {
				index = 1
			}
			return providers.Response{Content: []byte(fmt.Sprintf(`{"regions":[{"index":%d,"bounds":[0,0,%g,1]}]}`, index, .5+float64(index)*.2))}, nil
		}
		return stageFixture(learningFact("text")).Complete(context.Background(), r)
	})
	result, err := NewGenerator(p, Options{RenderPage: func(_ context.Context, file []byte, page int) ([]byte, error) {
		seenFiles = append(seenFiles, file)
		if string(file) == "one" && page != 1 || string(file) == "two" && page != 0 {
			t.Fatal("wrong local page")
		}
		return buf.Bytes(), nil
	}}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Name: "one.pdf", Data: []byte("one"), Pages: 2}, {Name: "two.pdf", Data: []byte("two"), Pages: 1}}})
	if err != nil || len(result.Images) != 2 || result.Images[0].SourceName != "two.pdf" || result.Images[0].SourcePage != 1 || result.Images[1].SourceName != "one.pdf" || result.Images[1].SourcePage != 2 {
		t.Fatalf("source mapping %+v %v", result.Images, err)
	}
}

func figurePDF(t *testing.T, rotation int) []byte {
	t.Helper()
	photo := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := range 20 {
		for x := range 20 {
			photo.Set(x, y, color.RGBA{R: 240, A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, photo, nil); err != nil {
		t.Fatal(err)
	}
	drawing := "q 60 0 0 60 25 65 cm /Photo Do Q\n0 0 0 RG 3 w 25 30 m 85 30 l 85 55 l S\nBT /F1 10 Tf 25 140 Td (Figure caption) Tj ET"
	objects := [][]byte{[]byte("<< /Type /Catalog /Pages 2 0 R >>"), []byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"), []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 120 180] /Rotate %d /Resources << /Font << /F1 5 0 R >> /XObject << /Photo 6 0 R >> >> /Contents 4 0 R >>", rotation)), []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(drawing), drawing)), []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"), append(append([]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 20 /Height 20 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", jpg.Len())), jpg.Bytes()...), []byte("\nendstream")...)}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n", i+1)
		pdf.Write(obj)
		pdf.WriteString("\nendobj\n")
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}
