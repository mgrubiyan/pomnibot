package ingest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // PNG decoding for photos sent as screenshots
	"math"

	_ "golang.org/x/image/webp" // WebP decoding: messengers store photos in it
)

// OCR service limits, enforced here rather than learned from an error.
const (
	MaxOCRBytes  = 10_000_000 // per file
	MaxOCRPixels = 20_000_000 // per image
	MaxOCRPages  = 200        // per PDF
)

// maxLongSide caps a photo that has to be shrunk anyway: 4096 px along the
// long side keeps handwriting legible and a JPEG well under the size limit.
const maxLongSide = 4096

// jpegQualities are tried in turn until the image fits the size limit.
var jpegQualities = []int{85, 75, 65, 55}

// limits are the service limits; tests shrink them.
type limits struct {
	bytes, pixels, pages int
}

var serviceLimits = limits{bytes: MaxOCRBytes, pixels: MaxOCRPixels, pages: MaxOCRPages}

// fitImage returns an image the OCR service accepts. A JPEG or PNG within the
// limits goes as is; another format, such as WebP, is re-encoded like a
// photo too large. A larger one, typical for a phone camera, is turned upright by
// its EXIF orientation (re-encoding drops EXIF), shrunk along the long side
// to fit the pixel limit, converted to grayscale and encoded as JPEG,
// lowering quality and then size until it fits the byte limit.
func fitImage(data []byte, lim limits) (out []byte, mime string, resized bool, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, fmt.Errorf("read image: %w", err)
	}
	mime = "image/" + format
	accepted := format == "jpeg" || format == "png"
	if accepted && cfg.Width*cfg.Height <= lim.pixels && len(data) <= lim.bytes {
		return data, mime, false, nil
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, fmt.Errorf("decode image: %w", err)
	}
	gray := orient(toGray(img), exifOrientation(data))

	w, h := gray.Bounds().Dx(), gray.Bounds().Dy()
	scale := math.Min(1, math.Sqrt(float64(lim.pixels)/float64(w*h)))
	scale = math.Min(scale, float64(maxLongSide)/float64(max(w, h)))
	for {
		nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
		small := gray
		if nw != w || nh != h {
			small = shrink(gray, nw, nh)
		}
		for _, q := range jpegQualities {
			var b bytes.Buffer
			if err := jpeg.Encode(&b, small, &jpeg.Options{Quality: q}); err != nil {
				return nil, "", false, fmt.Errorf("encode image: %w", err)
			}
			if b.Len() <= lim.bytes {
				return b.Bytes(), "image/jpeg", true, nil
			}
		}
		if nw == 1 && nh == 1 {
			return nil, "", false, fmt.Errorf("image does not fit %d bytes", lim.bytes)
		}
		scale *= 0.8
	}
}

func toGray(img image.Image) *image.Gray {
	if g, ok := img.(*image.Gray); ok {
		return g
	}
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(g, g.Bounds(), img, b.Min, draw.Src)
	return g
}

// shrink downscales by averaging the source pixels each target pixel covers:
// for text this keeps thin strokes that point sampling would lose.
func shrink(src *image.Gray, w, h int) *image.Gray {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := range w {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			sum, n := 0, 0
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := x0; sx < x1; sx++ {
					sum += int(row[sx])
				}
				n += x1 - x0
			}
			dst.Pix[y*dst.Stride+x] = uint8(sum / n)
		}
	}
	return dst
}

// orient turns the image upright for EXIF orientations 3 (180°), 6 (90°
// clockwise) and 8 (90° counterclockwise), the ones a phone camera writes.
func orient(src *image.Gray, orientation int) *image.Gray {
	if orientation != 3 && orientation != 6 && orientation != 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orientation != 3 {
		dw, dh = h, w
	}
	dst := image.NewGray(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			v := src.Pix[y*src.Stride+x]
			var dx, dy int
			switch orientation {
			case 3:
				dx, dy = w-1-x, h-1-y
			case 6:
				dx, dy = h-1-y, x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetGray(dx, dy, color.Gray{Y: v})
		}
	}
	return dst
}

// exifOrientation reads the orientation tag from a JPEG's APP1 Exif segment,
// 1 when there is none.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(data) {
			return 1 // start of scan: no Exif before the image data
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	entries := int(order.Uint16(t[ifd:]))
	for e := range entries {
		off := ifd + 2 + e*12
		if off+12 > len(t) {
			return 1
		}
		if order.Uint16(t[off:]) == 0x0112 {
			return int(order.Uint16(t[off+8:]))
		}
	}
	return 1
}
