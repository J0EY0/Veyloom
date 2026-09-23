package wiki

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// Files a project's wiki keeps as they are (docs/design.md 5.16): a
// picture, a spec, a data file people sent in the chat, which a page links
// to and says what is in. OKF bundles hold such files beside the concepts;
// they are no pages, so the index, the log and the checks leave them be,
// and a turn's commit carries them with its pages.

// AssetDir is the top-level folder of the files.
const AssetDir = "files"

// MaxAsset caps one file: a larger one stays in the chat, which the page's
// sources point back to.
const MaxAsset = 10 << 20

// AssetPath is where a file sent under filename goes for the page slug:
// /files/<slug>/<name>, the name lowercase ASCII words joined by hyphens,
// its extension kept. A markdown file keeps .markdown instead: every .md
// of a bundle is a concept to OKF, which a file sent as it is is not.
func AssetPath(pageSlug, filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	switch {
	case ext == ".md":
		ext = ".markdown"
	case !assetExt.MatchString(ext):
		ext = ""
	}
	name := slugWords(strings.TrimSuffix(path.Base(filename), path.Ext(filename)))
	if name == "" {
		name = "file"
	}
	return "/" + AssetDir + "/" + pageSlug + "/" + name + ext
}

// FreeAssetPath is AssetPath, numbered past the files already there.
func (b *Bundle) FreeAssetPath(pageSlug, filename string) string {
	p := AssetPath(pageSlug, filename)
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for n := 2; b.hasFile(p); n++ {
		p = fmt.Sprintf("%s-%d%s", base, n, ext)
	}
	return p
}

func (b *Bundle) hasFile(p string) bool {
	_, err := os.Lstat(b.file(p))
	return err == nil
}

// Asset reads a file the wiki keeps; ErrNotFound when there is none.
func (b *Bundle) Asset(p string) ([]byte, error) {
	p = path.Clean("/" + p)
	if !isAsset(p) {
		return nil, fmt.Errorf("%w: %s is not a file the wiki keeps", store.ErrNotFound, p)
	}
	data, err := os.ReadFile(b.file(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: the wiki has no file %s", store.ErrNotFound, p)
	}
	return data, err
}

// PutAsset writes a file the wiki keeps as it is. Pages go through Create
// and Put, which hold them to OKF.
func (w *Writer) PutAsset(p string, data []byte) error {
	p = path.Clean("/" + p)
	switch {
	case path.Ext(p) == ".md":
		return fmt.Errorf("%w: %s would be a page: keep a markdown file as .markdown", store.ErrInvalidInput, p)
	case !isAsset(p):
		return fmt.Errorf("%w: %s is not a file under /%s/", store.ErrInvalidInput, p, AssetDir)
	case len(data) > MaxAsset:
		return fmt.Errorf("%w: %s is %d MB; a file the wiki keeps should stay under %d MB", store.ErrInvalidInput, p, len(data)>>20, MaxAsset>>20)
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	w.touch(p)
	return writeFile(w.b.file(p), data)
}

// isAsset reports whether p names a file under /files/ that is no page and
// hides nothing.
func isAsset(p string) bool {
	return strings.HasPrefix(p, "/"+AssetDir+"/") && path.Ext(p) != ".md" && !strings.Contains(p, "/.")
}

// assetExt is an extension kept on a file's name: a dot and a few ASCII
// letters or digits.
var assetExt = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

// slugWords turns a name into lowercase ASCII words joined by hyphens;
// empty when it has none.
func slugWords(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	out := b.String()
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	return out
}
