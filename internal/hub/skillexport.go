package hub

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"maps"
	"slices"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// ExportSkill is the skill of the library called name as a zip file of its
// folder, in Agent Skills form (docs/design.md 5.15): what a runtime
// loads, for a person to take elsewhere, into ~/.claude/skills say, or up
// to a service that takes skills as zip files. The folder sits at the
// archive's root under the skill's name; scripts keep their executable bit.
func (h *Hub) ExportSkill(ctx context.Context, name string) ([]byte, error) {
	if !wikiSlug(name) {
		return nil, store.Invalid("skillBadName", store.Params{"name": name}, "%q is no skill name: lowercase letters, digits and hyphens", name)
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return nil, err
	}
	page, err := r.bundle.Page(wiki.SkillPath(name))
	if err != nil {
		return nil, err
	}
	skill, err := r.bundle.ProjectSkill(name)
	if err != nil {
		return nil, err
	}
	modified := page.Modified
	if modified.IsZero() {
		modified = time.Now()
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, rel := range slices.Sorted(maps.Keys(skill.Files)) {
		data := skill.Files[rel]
		header := &zip.FileHeader{Name: name + "/" + rel, Method: zip.Deflate, Modified: modified}
		mode := fs.FileMode(0o644)
		if slices.Contains(skill.Executable, rel) {
			mode = 0o755
		}
		header.SetMode(mode)
		w, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
