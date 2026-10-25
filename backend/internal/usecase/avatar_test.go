package usecase

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testImage(t *testing.T, w, h int, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(buf *bytes.Buffer, img image.Image) error { return png.Encode(buf, img) }
func encodeJPEG(buf *bytes.Buffer, img image.Image) error {
	return jpeg.Encode(buf, img, &jpeg.Options{Quality: 90})
}

func TestCleanAvatar(t *testing.T) {
	t.Run("re-encodes a PNG as JPEG of the same size", func(t *testing.T) {
		out, err := CleanAvatar(testImage(t, 256, 256, encodePNG))
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
		if err != nil || format != "jpeg" || cfg.Width != 256 || cfg.Height != 256 {
			t.Fatalf("got %s %dx%d, %v", format, cfg.Width, cfg.Height, err)
		}
	})

	t.Run("drops the metadata of a JPEG", func(t *testing.T) {
		in := testImage(t, 64, 64, encodeJPEG)
		// An EXIF segment, where phones keep the place a photo was taken.
		exif := append([]byte{0xFF, 0xE1, 0x00, 0x10}, []byte("Exif\x00\x00GPS-DATA")...)
		in = append(append(append([]byte{}, in[:2]...), exif...), in[2:]...)
		if _, _, err := image.DecodeConfig(bytes.NewReader(in)); err != nil {
			t.Fatalf("test image is broken: %v", err)
		}
		out, err := CleanAvatar(in)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPS-DATA")) {
			t.Fatal("metadata kept")
		}
	})

	for name, data := range map[string][]byte{
		"not an image": []byte("hello"),
		"empty":        nil,
		"too wide":     testImage(t, 1025, 8, encodePNG),
		"too tall":     testImage(t, 8, 1025, encodePNG),
		"too heavy":    append(testImage(t, 8, 8, encodePNG), make([]byte, maxAvatarBytes)...),
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := CleanAvatar(data); !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
		})
	}
}
