package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func picture(w, h int, alpha uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: alpha})
		}
	}
	return img
}

func save(t *testing.T, name string, write func(*bytes.Buffer) error) (string, int64) {
	t.Helper()
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, int64(buf.Len())
}

// withOrientation puts an EXIF block saying orientation o right after a
// JPEG's start marker, the way a phone camera writes one.
func withOrientation(t *testing.T, jpg []byte, o uint16) []byte {
	t.Helper()
	var tiff bytes.Buffer
	tiff.WriteString("II*\x00")
	binary.Write(&tiff, binary.LittleEndian, uint32(8))      //nolint:errcheck // a buffer
	binary.Write(&tiff, binary.LittleEndian, uint16(1))      //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, uint16(0x0112)) //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, uint16(3))      //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, uint32(1))      //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, o)              //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, uint16(0))      //nolint:errcheck
	binary.Write(&tiff, binary.LittleEndian, uint32(0))      //nolint:errcheck
	body := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := []byte{0xFF, 0xE1, byte((len(body) + 2) >> 8), byte(len(body) + 2)}
	out := append([]byte{0xFF, 0xD8}, segment...)
	out = append(out, body...)
	return append(out, jpg[2:]...)
}

func TestInspect(t *testing.T) {
	pngPath, _ := save(t, "a.png", func(b *bytes.Buffer) error { return png.Encode(b, picture(300, 200, 255)) })
	if p, ok := Inspect(pngPath); !ok || p.Width != 300 || p.Height != 200 || p.Format != "png" || p.Orientation != 1 {
		t.Errorf("png: %+v %v", p, ok)
	}
	gifPath, _ := save(t, "a.gif", func(b *bytes.Buffer) error { return gif.Encode(b, picture(40, 30, 255), nil) })
	if p, ok := Inspect(gifPath); !ok || p.Width != 40 || p.Height != 30 || p.Format != "gif" {
		t.Errorf("gif: %+v %v", p, ok)
	}
	// A photo taken with the phone on its side is shown upright: its
	// width and height swap.
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, picture(400, 100, 255), nil); err != nil {
		t.Fatal(err)
	}
	turned, _ := save(t, "turned.jpg", func(b *bytes.Buffer) error { _, err := b.Write(withOrientation(t, jpg.Bytes(), 6)); return err })
	if p, ok := Inspect(turned); !ok || p.Width != 100 || p.Height != 400 || p.Orientation != 6 {
		t.Errorf("turned jpeg: %+v %v", p, ok)
	}
	text, _ := save(t, "notes.txt", func(b *bytes.Buffer) error { _, err := b.WriteString("not a picture"); return err })
	if _, ok := Inspect(text); ok {
		t.Error("a text file read as a picture")
	}
}

func TestThumbnail(t *testing.T) {
	dir := t.TempDir()
	thumbOf := func(src string, size int64) (string, image.Config, string) {
		t.Helper()
		out, err := Thumbnail(src, filepath.Join(dir, filepath.Base(src)+".thumb"), size)
		if err != nil {
			t.Fatal(err)
		}
		if out == "" {
			return "", image.Config{}, ""
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cfg, format, err := image.DecodeConfig(f)
		if err != nil {
			t.Fatal(err)
		}
		return out, cfg, format
	}

	// A big screenshot becomes a JPEG as wide as a thumbnail.
	big, size := save(t, "big.png", func(b *bytes.Buffer) error { return png.Encode(b, picture(1600, 1000, 255)) })
	if out, cfg, format := thumbOf(big, size); filepath.Ext(out) != ".jpg" || format != "jpeg" || cfg.Width != 640 || cfg.Height != 400 {
		t.Errorf("big png: %s %+v %s", out, cfg, format)
	}
	// One with see-through parts stays a PNG, tall ones are as tall as a
	// thumbnail.
	clear, size := save(t, "clear.png", func(b *bytes.Buffer) error { return png.Encode(b, picture(500, 1000, 128)) })
	if out, cfg, format := thumbOf(clear, size); filepath.Ext(out) != ".png" || format != "png" || cfg.Width != 320 || cfg.Height != 640 {
		t.Errorf("clear png: %s %+v %s", out, cfg, format)
	}
	// A small one is shown as it is; so is a GIF, which may move.
	small, size := save(t, "small.png", func(b *bytes.Buffer) error { return png.Encode(b, picture(200, 100, 255)) })
	if out, _, _ := thumbOf(small, size); out != "" {
		t.Errorf("small png got a thumbnail %s", out)
	}
	moving, size := save(t, "moving.gif", func(b *bytes.Buffer) error { return gif.Encode(b, picture(1200, 900, 255), nil) })
	if out, _, _ := thumbOf(moving, size); out != "" {
		t.Errorf("gif got a thumbnail %s", out)
	}
	// A photo on its side is turned upright in its thumbnail, even a small
	// one.
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, picture(400, 100, 255), nil); err != nil {
		t.Fatal(err)
	}
	turned, size := save(t, "turned.jpg", func(b *bytes.Buffer) error { _, err := b.Write(withOrientation(t, jpg.Bytes(), 6)); return err })
	if out, cfg, _ := thumbOf(turned, size); out == "" || cfg.Width != 100 || cfg.Height != 400 {
		t.Errorf("turned jpeg: %s %+v", out, cfg)
	}
	// No temporary files are left beside the thumbnails.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".jpg" && filepath.Ext(entry.Name()) != ".png" {
			t.Errorf("left behind: %s", entry.Name())
		}
	}
}

func TestOrient(t *testing.T) {
	// A 2×1 image: red then blue.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	src.SetRGBA(0, 0, red)
	src.SetRGBA(1, 0, blue)
	cases := map[int][]color.RGBA{
		// orientation: the pixels read row by row afterwards
		1: {red, blue},
		2: {blue, red},
		3: {blue, red},
		6: {red, blue}, // a quarter clockwise: a column, red on top
		8: {blue, red}, // anticlockwise: blue on top
	}
	for o, want := range cases {
		got := orient(src, o)
		var pixels []color.RGBA
		b := got.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				pixels = append(pixels, got.RGBAAt(x, y))
			}
		}
		if len(pixels) != len(want) || pixels[0] != want[0] || pixels[1] != want[1] {
			t.Errorf("orientation %d: %v, want %v", o, pixels, want)
		}
		if o >= 5 && (b.Dx() != 1 || b.Dy() != 2) {
			t.Errorf("orientation %d: %v, want a column", o, b)
		}
	}
}
