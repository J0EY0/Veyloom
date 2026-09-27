// Package media reads what the hub needs to know about a picture a person
// uploads: its size in pixels, the way a browser will show it, and a
// smaller copy for the chat and the attachments tab (docs/webui.md 4.21).
package media

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registers GIF with image.DecodeConfig
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP
)

// ThumbnailEdge is the longest side of a thumbnail in pixels: twice what
// the chat shows a picture at, so it stays sharp on a dense screen.
const ThumbnailEdge = 640

// maxPixels bounds the pictures decoded whole to make a thumbnail; a
// bigger one would take too much memory, and is shown as it is.
const maxPixels = 40_000_000

// smallFile is the size in bytes under which a picture no bigger than a
// thumbnail in pixels is shown as it is.
const smallFile = 256 << 10

// Picture is what an uploaded picture is, as far as showing it goes.
type Picture struct {
	// Width and Height are as a browser shows it: turned the way the
	// camera's EXIF orientation says.
	Width, Height int
	// Format is the decoder's name: "png", "jpeg", "gif" or "webp".
	Format string
	// Orientation is the EXIF orientation, 1 to 8; 1 for none.
	Orientation int
}

// Inspect reads a picture's size and format without decoding it. ok is
// false for a file that is not a PNG, JPEG, GIF or WebP.
func Inspect(path string) (Picture, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Picture{}, false
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return Picture{}, false
	}
	p := Picture{Width: cfg.Width, Height: cfg.Height, Format: format, Orientation: 1}
	if format == "jpeg" {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			p.Orientation = exifOrientation(f)
		}
		if p.Orientation >= 5 {
			p.Width, p.Height = p.Height, p.Width
		}
	}
	return p, true
}

// Thumbnail writes a smaller copy of the picture at src beside it, at base
// plus the extension of the format it takes, and returns that path. It
// writes nothing and returns "" for a picture small enough to show as it
// is, an animated one (a GIF keeps moving), or one too big to decode.
// size is the file's size in bytes.
func Thumbnail(src, base string, size int64) (string, error) {
	p, ok := Inspect(src)
	if !ok || p.Format == "gif" || p.Width*p.Height > maxPixels {
		return "", nil
	}
	if p.Width <= ThumbnailEdge && p.Height <= ThumbnailEdge && size <= smallFile && p.Orientation == 1 {
		return "", nil
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		// Its header read but its pixels do not: show it as it is.
		return "", nil
	}

	// Scale in the file's own orientation, then turn the small copy.
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > ThumbnailEdge || h > ThumbnailEdge {
		if w >= h {
			w, h = ThumbnailEdge, max(1, h*ThumbnailEdge/w)
		} else {
			w, h = max(1, w*ThumbnailEdge/h), ThumbnailEdge
		}
	}
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(small, small.Bounds(), img, b, xdraw.Over, nil)
	out := orient(small, p.Orientation)

	// A picture with see-through parts stays a PNG; any other is a JPEG.
	path := base + ".jpg"
	encode := func(w io.Writer) error { return jpeg.Encode(w, out, &jpeg.Options{Quality: 82}) }
	if !out.Opaque() {
		path = base + ".png"
		encode = func(w io.Writer) error { return png.Encode(w, out) }
	}
	if err := writeFile(path, encode); err != nil {
		return "", err
	}
	return path, nil
}

// writeFile writes through a temporary file beside path, so a reader never
// sees half a picture.
func writeFile(path string, encode func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".thumb-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after the rename
	if err := encode(tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("encode thumbnail: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// orient turns an image the way an EXIF orientation says a viewer should
// show it: 2 mirrors it, 3 turns it half round, 4 flips it, 5 and 7 mirror
// it across a diagonal, 6 turns it a quarter clockwise, 8 anticlockwise.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// exifOrientation reads the orientation a JPEG's EXIF block gives, 1 when
// it gives none or cannot be read.
func exifOrientation(r io.Reader) int {
	o, err := readOrientation(r)
	if err != nil || o < 1 || o > 8 {
		return 1
	}
	return o
}

var errNoOrientation = errors.New("no orientation")

func readOrientation(r io.Reader) (int, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil || head != [2]byte{0xFF, 0xD8} {
		return 0, errNoOrientation
	}
	for {
		var marker [4]byte
		if _, err := io.ReadFull(r, marker[:]); err != nil || marker[0] != 0xFF {
			return 0, errNoOrientation
		}
		length := int(marker[2])<<8 | int(marker[3])
		if length < 2 {
			return 0, errNoOrientation
		}
		switch marker[1] {
		case 0xE1: // APP1, where EXIF lives
			body := make([]byte, length-2)
			if _, err := io.ReadFull(r, body); err != nil {
				return 0, errNoOrientation
			}
			if o, ok := orientationIn(body); ok {
				return o, nil
			}
		case 0xDA, 0xD9: // the pixels begin, or the file ends: no EXIF
			return 0, errNoOrientation
		default:
			if _, err := io.CopyN(io.Discard, r, int64(length-2)); err != nil {
				return 0, errNoOrientation
			}
		}
	}
}

// orientationIn finds tag 0x0112 in the first directory of an APP1
// segment's EXIF data.
func orientationIn(b []byte) (int, bool) {
	if len(b) < 14 || string(b[:6]) != "Exif\x00\x00" {
		return 0, false
	}
	t := b[6:]
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 }
		u32 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 | int(p[3])<<24 }
	case "MM":
		u16 = func(p []byte) int { return int(p[0])<<8 | int(p[1]) }
		u32 = func(p []byte) int { return int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3]) }
	default:
		return 0, false
	}
	off := u32(t[4:8])
	if off < 8 || off+2 > len(t) {
		return 0, false
	}
	n := u16(t[off : off+2])
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 0, false
		}
		if u16(t[e:e+2]) == 0x0112 {
			return u16(t[e+8 : e+10]), true
		}
	}
	return 0, false
}
