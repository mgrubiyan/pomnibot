package usecase

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
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

	t.Run("puts a transparent PNG on white", func(t *testing.T) {
		seeThrough := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		var buf bytes.Buffer
		if err := png.Encode(&buf, seeThrough); err != nil {
			t.Fatal(err)
		}
		out, err := CleanAvatar(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if r, g, b, _ := img.At(4, 4).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
			t.Fatalf("transparent pixel came out as %d,%d,%d", r>>8, g>>8, b>>8)
		}
	})

	for name, data := range map[string][]byte{
		"not an image": []byte("hello"),
		"empty":        nil,
		"too wide":     testImage(t, 1025, 8, encodePNG),
		"too tall":     testImage(t, 8, 1025, encodePNG),
		"too heavy":    append(testImage(t, 8, 8, encodePNG), make([]byte, maxAvatarBytes)...),
		// Its decoder is linked in here, as WebP's is elsewhere in the binary.
		"a GIF": testImage(t, 8, 8, func(buf *bytes.Buffer, img image.Image) error { return gif.Encode(buf, img, nil) }),
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := CleanAvatar(data); !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
		})
	}
}

type importQuerier struct {
	db.Querier
	saved db.SaveImportedAvatarParams
	calls int
}

func (q *importQuerier) SaveImportedAvatar(_ context.Context, arg db.SaveImportedAvatarParams) (db.User, error) {
	q.calls++
	q.saved = arg
	return db.User{ID: arg.ID, AvatarID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, AvatarSource: arg.Source}, nil
}

// pngHeader is a PNG that claims the given size and holds no pixels.
func pngHeader(t *testing.T, w, h uint32) []byte {
	t.Helper()
	data := testImage(t, 1, 1, encodePNG)
	// IHDR follows the 8-byte signature: length, type, width, height, …
	binary.BigEndian.PutUint32(data[16:], w)
	binary.BigEndian.PutUint32(data[20:], h)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestImportAvatar(t *testing.T) {
	id := uuid.New()
	const source = "https://i.max.ru/photo.png"

	t.Run("a wide transparent photo is cut square, scaled down and flattened", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
		// Left and right thirds red, the middle transparent: the square keeps the middle.
		for x := range 1200 {
			for y := range 800 {
				if x < 200 || x >= 1000 {
					img.Set(x, y, color.NRGBA{R: 255, A: 255})
				}
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		q := &importQuerier{}
		account, err := (&userServiceImpl{querier: q}).ImportAvatar(context.Background(), id, " "+source+" ", buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if q.saved.ID.Bytes != id || q.saved.Source.String != source || account.AvatarSource != source || !account.User.AvatarUrl.Set {
			t.Fatalf("saved %+v, account %+v", q.saved, account)
		}
		out, format, err := image.Decode(bytes.NewReader(q.saved.Image))
		if err != nil || format != "jpeg" || out.Bounds().Dx() != 512 || out.Bounds().Dy() != 512 {
			t.Fatalf("saved %s %v, %v", format, out.Bounds(), err)
		}
		for _, x := range []int{4, 256, 507} {
			if r, g, b, _ := out.At(x, 256).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
				t.Fatalf("at x=%d the transparent middle is not white: %d %d %d", x, r>>8, g>>8, b>>8)
			}
		}
	})

	t.Run("a small JPEG keeps its size", func(t *testing.T) {
		q := &importQuerier{}
		if _, err := (&userServiceImpl{querier: q}).ImportAvatar(context.Background(), id, source, testImage(t, 300, 200, encodeJPEG)); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(q.saved.Image))
		if err != nil || cfg.Width != 200 || cfg.Height != 200 {
			t.Fatalf("saved %dx%d, %v", cfg.Width, cfg.Height, err)
		}
	})

	for name, data := range map[string][]byte{
		"not an image":             []byte("<svg></svg>"),
		"a GIF":                    testImage(t, 10, 10, func(b *bytes.Buffer, i image.Image) error { return gif.Encode(b, i, nil) }),
		"a header asking for 30k²": pngHeader(t, 30000, 30000),
	} {
		t.Run(name+" is refused before saving", func(t *testing.T) {
			q := &importQuerier{}
			_, err := (&userServiceImpl{querier: q}).ImportAvatar(context.Background(), id, source, data)
			if !errors.Is(err, ErrValidation) || q.calls != 0 {
				t.Fatalf("err %v, saves %d", err, q.calls)
			}
		})
	}
}
