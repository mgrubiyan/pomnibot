package ingest

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"math"
	"math/rand/v2"
	"testing"
)

// withOrientation inserts an Exif segment with the given orientation right
// after the JPEG start marker.
func withOrientation(jpg []byte, orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("II")
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1)) // one entry
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(orientation))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0)) // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write(jpg[:2])
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(jpg[2:])
	return out.Bytes()
}

func TestExifOrientation(t *testing.T) {
	jpg := jpegBytes(t, 16, 8)
	if got := exifOrientation(jpg); got != 1 {
		t.Errorf("no Exif: orientation %d, want 1", got)
	}
	if got := exifOrientation(withOrientation(jpg, 6)); got != 6 {
		t.Errorf("orientation %d, want 6", got)
	}
	if got := exifOrientation(pngBytes(t, 4, 4)); got != 1 {
		t.Errorf("PNG: orientation %d, want 1", got)
	}
}

func TestFitImageTurnsPhotoUpright(t *testing.T) {
	// A portrait photo stored landscape, marked "rotate 90° clockwise".
	jpg := withOrientation(jpegBytes(t, 200, 100), 6)
	out, _, resized, err := fitImage(jpg, limits{bytes: MaxOCRBytes, pixels: 5000})
	if err != nil || !resized {
		t.Fatalf("fitImage() resized=%v err=%v", resized, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Height <= cfg.Width {
		t.Errorf("%dx%d, want portrait: re-encoding drops Exif, so the pixels must be turned", cfg.Width, cfg.Height)
	}
}

// noisePNG is an image that does not compress, like a photo.
func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.IntN(256))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFitImageFitsByteLimit(t *testing.T) {
	big := noisePNG(t, 400, 400) // few pixels, but heavy
	lim := limits{bytes: len(big) / 4, pixels: MaxOCRPixels}
	out, mime, resized, err := fitImage(big, lim)
	if err != nil {
		t.Fatalf("fitImage() error = %v", err)
	}
	if !resized || mime != "image/jpeg" || len(out) > lim.bytes {
		t.Errorf("resized=%v mime=%s size=%d, want a JPEG within %d bytes", resized, mime, len(out), lim.bytes)
	}
}

func TestShrinkAveragesPixels(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 4, 2))
	copy(src.Pix, []uint8{0, 100, 200, 200, 100, 200, 0, 0})
	dst := shrink(src, 2, 1)
	if got := dst.Pix; got[0] != 100 || got[1] != 100 {
		t.Errorf("shrink() = %v, want the mean of each 2x2 block: [100 100]", got)
	}
}

// overLimit is the side of a square image just over MaxDecodePixels: if the
// check is lost, decoding it costs tens of megabytes, not gigabytes.
var overLimit = uint32(math.Sqrt(MaxDecodePixels)) + 1

// bombPNG is the start of a PNG whose header announces w×h pixels: enough
// for DecodeConfig, while Decode would allocate the whole bitmap.
func bombPNG(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 1, 0, 0, 0, 0) // 1-bit grayscale
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestFitImageRejectsBombBeforeDecoding(t *testing.T) {
	_, _, _, err := fitImage(bombPNG(overLimit, overLimit), serviceLimits)
	if !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("fitImage() error = %v, want ErrImageTooLarge", err)
	}
}
