package hub

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// Skills a person sends from the browser (docs/design.md 5.15): a zip
// file, such as the library's own export or one a colleague shared, or the
// files of a folder they picked or dropped. What they hold is laid out in a
// folder of its own and every skill found there is imported as from a
// folder on this machine.

// Limits of one upload, which may hold several skills: four skills' worth.
const (
	MaxUploadBytes = 4 * wiki.MaxSkillBytes
	MaxUploadFiles = 4 * wiki.MaxSkillFiles
)

// SkillUpload is what came from the browser: a zip file, or the files of a
// folder by their paths, which start with the folder's name.
type SkillUpload struct {
	// Name is the zip file's name, which a skill at its root is named after
	// when its SKILL.md names none.
	Name    string
	Zip     io.ReaderAt
	ZipSize int64
	Files   []UploadFile
	// Team is the project whose team looks after what comes in, or "".
	Team   string
	UserID string
}

// UploadFile is one file of a folder sent from the browser.
type UploadFile struct {
	Path string
	Open func() (io.ReadCloser, error)
}

// UploadedSkill is how one skill of an upload went: the page it got, or
// why it did not come in, by the code and params the import refused it
// with.
type UploadedSkill struct {
	Name    string       `json:"name"`
	Path    string       `json:"path,omitempty"`
	Code    string       `json:"code,omitempty"`
	Params  store.Params `json:"params,omitempty"`
	Message string       `json:"message,omitempty"`
}

// UploadSkills imports the skills an upload holds: its root when it holds
// a SKILL.md, else each folder that does, down to a skills repository's
// <repo>/skills/<name>/, not looking inside a skill for more.
func (h *Hub) UploadSkills(ctx context.Context, up SkillUpload) ([]UploadedSkill, error) {
	tmp, err := os.MkdirTemp("", "veyloom-upload-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	root := tmp
	var laid uploadLayout
	if up.Zip != nil {
		base := strings.TrimSuffix(filepath.Base(up.Name), filepath.Ext(up.Name))
		if base == "" || base == "." || strings.HasPrefix(base, ".") {
			base = "upload"
		}
		root = filepath.Join(tmp, base)
		err = laid.unzip(root, up.Zip, up.ZipSize)
	} else {
		err = laid.folder(root, up.Files)
	}
	if err != nil {
		return nil, err
	}
	folders := skillFolders(root, 3)
	if len(folders) == 0 {
		return nil, store.Invalid("uploadNoSkill", nil, "the upload holds no folder with a SKILL.md")
	}
	from := "an upload"
	if up.Name != "" {
		from = "the upload " + filepath.Base(up.Name)
	}
	out := make([]UploadedSkill, 0, len(folders))
	for _, folder := range folders {
		got := UploadedSkill{Name: filepath.Base(folder)}
		page, err := h.importSkill(ctx, folder, from, up.Team, up.UserID)
		var p *store.Problem
		switch {
		case err == nil:
			got.Name, got.Path = wiki.SkillName(page.Path), page.Path
		case errors.As(err, &p):
			got.Code, got.Params, got.Message = p.Code, p.Params, p.Text
			if name, ok := p.Params["name"]; ok && name != "" {
				got.Name = name
			}
		default:
			return nil, err
		}
		out = append(out, got)
	}
	return out, nil
}

// uploadLayout counts what an upload lays out, against its limits.
type uploadLayout struct {
	files int
	bytes int64
}

func (l *uploadLayout) tooMuch() error {
	return store.Invalid("uploadTooLarge", store.Params{"mb": wiki.MB(MaxUploadBytes), "files": strconv.Itoa(MaxUploadFiles)},
		"an upload holds at most %d files and %s MB", MaxUploadFiles, wiki.MB(MaxUploadBytes))
}

// write lays out one file at rel under root, counting its bytes as they
// come, not as a header claims; executable, it runs there too.
func (l *uploadLayout) write(root, rel string, r io.Reader, executable bool) error {
	if l.files++; l.files > MaxUploadFiles {
		return l.tooMuch()
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return store.Invalid("uploadBadPath", store.Params{"path": rel}, "%s comes twice in the upload", rel)
	}
	n, err := io.Copy(f, io.LimitReader(r, MaxUploadBytes-l.bytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if l.bytes += n; l.bytes > MaxUploadBytes {
		return l.tooMuch()
	}
	return nil
}

// unzip lays out a zip file under root: its files, those made to run as
// such, not its links; a path leaving the folder refuses it all. The
// folders a Mac adds to a zip it makes are left out.
func (l *uploadLayout) unzip(root string, r io.ReaderAt, size int64) error {
	archive, err := zip.NewReader(r, size)
	if err != nil {
		return store.Invalid("uploadNotZip", nil, "the upload is no zip file: %v", err)
	}
	for _, f := range archive.File {
		name := strings.TrimSuffix(f.Name, "/")
		if f.FileInfo().IsDir() || name == "" {
			continue
		}
		if !uploadPath(name) {
			return store.Invalid("uploadBadPath", store.Params{"path": f.Name}, "%s leaves the upload's folder", f.Name)
		}
		if f.Mode()&fs.ModeSymlink != 0 || name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return store.Invalid("uploadNotZip", nil, "%s does not read: %v", f.Name, err)
		}
		err = l.write(root, name, rc, f.Mode()&0o111 != 0)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// folder lays out the files of a folder under root, by their paths. A
// browser tells nothing of which of them run: scripts opening with #! do
// anyway (wiki.Bundle.ProjectSkill), a built tool needs a zip.
func (l *uploadLayout) folder(root string, files []UploadFile) error {
	if len(files) == 0 {
		return store.Invalid("uploadNoSkill", nil, "the upload holds no files")
	}
	for _, f := range files {
		if !uploadPath(f.Path) {
			return store.Invalid("uploadBadPath", store.Params{"path": f.Path}, "%s leaves the upload's folder", f.Path)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = l.write(root, f.Path, rc, false)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// uploadPath reports whether p, a path in an upload, stays within it.
func uploadPath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\\x00") || path.IsAbs(p) {
		return false
	}
	clean := path.Clean(p)
	return clean == p && clean != ".." && !strings.HasPrefix(clean, "../")
}

// skillFolders are the folders under root, root itself among them, that
// hold a SKILL.md, depth folders down at most; a skill's own folders are
// not looked in, nor hidden ones.
func skillFolders(root string, depth int) []string {
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
		return []string{root}
	}
	if depth == 0 {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "__MACOSX" {
			out = append(out, skillFolders(filepath.Join(root, e.Name()), depth-1)...)
		}
	}
	return out
}
