package hub

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The bundles a project's wiki mounts (docs/design.md 5.9): OKF bundles
// from elsewhere, a company's data catalog say, that a person points the
// project at by their folders. They are read-only: searched and read with
// the project's wiki, never written, and a page of one is addressed as
// /@<mount>/<its path in the bundle>, the mount named after its folder.

// mountPrefix starts the path of a page in a mounted bundle.
const mountPrefix = "/@"

// maxMounts caps the bundles one project mounts.
const maxMounts = 8

// mountedWiki is one bundle a project mounts.
type mountedWiki struct {
	// name is the mount's name in paths, from its folder.
	name   string
	dir    string
	bundle *wiki.Bundle
	// err says why the bundle could not be opened; bundle is nil then.
	err error
}

// mountPath is the path, as Veyloom addresses it, of the page p of the
// mount named name.
func mountPath(name, p string) string { return mountPrefix + name + p }

// splitMount says which mount a path is in and the page's path in that
// bundle; ok is false for a path of the project's own wiki.
func splitMount(p string) (name, inner string, ok bool) {
	rest, found := strings.CutPrefix(p, mountPrefix)
	if !found {
		return "", "", false
	}
	name, inner, found = strings.Cut(rest, "/")
	if !found || name == "" {
		return "", "", false
	}
	return name, "/" + inner, true
}

var mountNameJunk = regexp.MustCompile(`[^a-z0-9]+`)

// mountNames names each folder: its base name as lowercase words joined by
// hyphens, numbered when two come out the same.
func mountNames(dirs []string) []string {
	names := make([]string, len(dirs))
	taken := map[string]int{}
	for i, dir := range dirs {
		name := strings.Trim(mountNameJunk.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-")
		if name == "" {
			name = "bundle"
		}
		taken[name]++
		if n := taken[name]; n > 1 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		names[i] = name
	}
	return names
}

// CleanWikiMounts checks the folders a person gives a project's wiki to
// mount and returns them tidied: absolute, existing folders, each once,
// at most maxMounts. It is the API's to call before storing them.
func CleanWikiMounts(dirs []string) ([]string, error) {
	var out []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			return nil, store.Invalid("mountNotAbsolute", store.Params{"path": dir}, "%s is not a full path: a mounted bundle is named by its folder from the root, like /data/catalog", dir)
		}
		dir = filepath.Clean(dir)
		if slices.Contains(out, dir) {
			continue
		}
		if !isDir(dir) {
			return nil, store.Invalid("mountNotFolder", store.Params{"path": dir}, "%s is not a folder on this machine", dir)
		}
		out = append(out, dir)
	}
	if len(out) > maxMounts {
		return nil, store.Invalid("mountTooMany", store.Params{"max": strconv.Itoa(maxMounts)}, "a project mounts at most %d bundles", maxMounts)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// mounts opens the bundles a project mounts, read-only, as they are on disk
// now. One that cannot be opened comes back with its error.
func (s *wikiShelf) mounts(ctx context.Context, p store.Project) []mountedWiki {
	if s == nil || s.root == "" || len(p.WikiExternalBundles) == 0 {
		return nil
	}
	names := mountNames(p.WikiExternalBundles)
	out := make([]mountedWiki, 0, len(names))
	for i, dir := range p.WikiExternalBundles {
		m := mountedWiki{name: names[i], dir: dir}
		m.bundle, m.err = s.mounted(ctx, dir)
		out = append(out, m)
	}
	return out
}

// mounted opens the bundle in dir read-only, once, and picks up what
// changed in it since.
func (s *wikiShelf) mounted(ctx context.Context, dir string) (*wiki.Bundle, error) {
	s.mu.Lock()
	b := s.foreign[dir]
	s.mu.Unlock()
	if b == nil {
		if !isDir(dir) {
			return nil, store.Missing("mountNotFolder", store.Params{"path": dir}, "%s is not a folder on this machine", dir)
		}
		opened, err := wiki.Open(ctx, dir, wiki.Options{ReadOnly: true})
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.foreign[dir] == nil {
			s.foreign[dir] = opened
		}
		b = s.foreign[dir]
		s.mu.Unlock()
		return b, nil
	}
	if _, err := b.Sync(ctx); err != nil {
		s.logger.Warn("read a mounted bundle", "dir", dir, "err", err)
	}
	return b, nil
}

// mountNamed finds the open mount of that name.
func mountNamed(mounts []mountedWiki, name string) (mountedWiki, error) {
	for _, m := range mounts {
		if m.name == name {
			if m.err != nil {
				return m, store.Missing("mountUnreadable", store.Params{"name": name}, "the mounted bundle %s cannot be read: %v", name, m.err)
			}
			return m, nil
		}
	}
	return mountedWiki{}, fmt.Errorf("%w: this project mounts no bundle %s", store.ErrNotFound, name)
}

// mountLinks rewrites the links of the page p of a mount to the paths
// Veyloom gives its pages, as rootLinks does for the project's own.
func mountLinks(name, p, body string) string {
	return okf.RewriteLinks(body, func(target string) (string, bool) {
		to, ok := okf.Resolve(p, target)
		if !ok || path.Ext(to) != ".md" {
			return "", false
		}
		return (&url.URL{Path: mountPath(name, to)}).EscapedPath(), true
	})
}

// isDir reports whether dir is a folder.
func isDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// readOnlyMount is what a write to a mounted page is told.
func readOnlyMount(p string) error {
	return fmt.Errorf("%w: %s is in a bundle the project mounts, which is read-only: write to the project's own wiki instead", store.ErrInvalidInput, p)
}

// searchMounted finds pages of a wiki and of the bundles it mounts by their
// text, best first; a page of the wiki's own wins a tie.
func searchMounted(own *wiki.Bundle, mounts []mountedWiki, query string, limit int) []wiki.Hit {
	return acrossMounts(own, mounts, limit, func(b *wiki.Bundle) []wiki.Hit { return b.Search(query, limit) })
}

// nearMounted finds the pages of a wiki and of the bundles it mounts that
// share words with text, best first, the way a brief finds related pages
// (wiki.Bundle.Relevant): what a search no page matches as written can
// offer, a question asked whole in Chinese above all, which has no spaces
// to split its words at.
func nearMounted(own *wiki.Bundle, mounts []mountedWiki, text string, limit int) []wiki.Hit {
	r := wiki.Relevance{Text: text}
	return acrossMounts(own, mounts, limit, func(b *wiki.Bundle) []wiki.Hit { return b.Relevant(r, limit) })
}

// acrossMounts gathers what find gives for a wiki and for each bundle it
// mounts, the mounted pages under their mounts' paths, best first; a page
// of the wiki's own wins a tie.
func acrossMounts(own *wiki.Bundle, mounts []mountedWiki, limit int, find func(*wiki.Bundle) []wiki.Hit) []wiki.Hit {
	hits := find(own)
	for _, m := range mounts {
		if m.bundle == nil {
			continue
		}
		for _, h := range find(m.bundle) {
			h.Path = mountPath(m.name, h.Path)
			hits = append(hits, h)
		}
	}
	slices.SortStableFunc(hits, func(a, b wiki.Hit) int { return b.Score() - a.Score() })
	return hits[:min(len(hits), limit)]
}

// mountedHealth is the wiki's health check with its links into the bundles
// it mounts taken for what they are: a link to a page a mount has is not
// broken, though the wiki itself has no such page.
func mountedHealth(h wiki.Health, mounts []mountedWiki) wiki.Health {
	var broken []wiki.BrokenLink
	for _, l := range h.Broken {
		if name, inner, ok := splitMount(l.To); ok {
			if m, err := mountNamed(mounts, name); err == nil {
				if _, err := m.bundle.Page(inner); err == nil {
					continue
				}
			}
		}
		broken = append(broken, l)
	}
	h.Broken = broken
	return h
}

// mountsLine says, for a brief, which bundles the wiki mounts.
func mountsLine(mounts []mountedWiki) string {
	var parts []string
	for _, m := range mounts {
		if m.bundle != nil {
			parts = append(parts, fmt.Sprintf("%s (%s)", m.name, count(len(m.bundle.Pages()), "page")))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "The project wiki also mounts bundles from elsewhere, read-only: " + strings.Join(parts, ", ") +
		". search_wiki finds their pages with the wiki's own, at paths like " + mountPrefix + "<bundle>/..., and read_wiki reads them; they are not written."
}

// readMounted reads a page of a mounted bundle for an agent, as read_wiki
// reads one of the wiki's own: the file as it is, then what links to it.
func readMounted(mounts []mountedWiki, name, inner string) (string, error) {
	mount, err := mountNamed(mounts, name)
	if err != nil {
		return "", err
	}
	page, err := mount.bundle.Page(inner)
	if err != nil {
		return "", fmt.Errorf("the mounted bundle %s has no page %s; search_wiki finds the ones it has", name, inner)
	}
	text, err := page.Doc.Bytes()
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Write(text)
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	if from := mount.bundle.Backlinks(page.Path); len(from) > 0 {
		for i, p := range from {
			from[i] = mountPath(name, p)
		}
		fmt.Fprintf(&sb, "\n(linked from: %s)\n", strings.Join(from, ", "))
	}
	fmt.Fprintf(&sb, "(from the bundle %s the project mounts, read-only; its links are paths in that bundle, under %s)\n", mount.dir, mountPath(name, "/"))
	return sb.String(), nil
}
