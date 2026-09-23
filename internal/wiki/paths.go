package wiki

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// CleanPath turns a page path from a tool or the web UI into the form
// pages are known by: from the bundle root, with a leading slash, like
// "/decisions/approvals-payload-json.md". It refuses paths that leave the
// bundle, hidden files, reserved files and anything that is not markdown.
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("%w: page path %q", store.ErrInvalidInput, p)
	}
	rel := path.Clean(strings.TrimPrefix(p, "/"))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%w: page path %q leaves the bundle", store.ErrInvalidInput, p)
	}
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", fmt.Errorf("%w: page path %q has a hidden part", store.ErrInvalidInput, p)
		}
	}
	if path.Ext(rel) != ".md" {
		return "", fmt.Errorf("%w: page path %q should end in .md", store.ErrInvalidInput, p)
	}
	if base := path.Base(rel); base == okf.IndexFile || base == okf.LogFile {
		return "", store.Invalid("reservedFile", store.Params{"name": base}, "%s is kept by Veyloom, not written as a page", base)
	}
	return "/" + rel, nil
}

// slug is how Veyloom names what it creates: lowercase ASCII words joined
// by hyphens, so paths read the same in every tool and shell.
var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// checkNewPath holds new pages to the naming rule: every directory and the
// file name are slugs, except that a skill's file is SKILL.md.
func checkNewPath(p string) error {
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, seg := range segs {
		if i == len(segs)-1 {
			if seg == okf.SkillFile {
				continue
			}
			seg = strings.TrimSuffix(seg, ".md")
		}
		if !slug.MatchString(seg) {
			return fmt.Errorf("%w: %q in %s should be lowercase letters, digits and hyphens", store.ErrInvalidInput, seg, p)
		}
	}
	return nil
}

// topDir is the top-level directory a page is in, "" for the bundle root.
func topDir(p string) string {
	rel := strings.TrimPrefix(p, "/")
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

// stem is a page's file name without .md, the title of last resort.
func stem(p string) string { return strings.TrimSuffix(path.Base(p), ".md") }
