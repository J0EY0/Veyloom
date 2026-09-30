// Package wiki keeps Veyloom's two wikis (design.md 5.3 to 5.5 and 5.9 to
// 5.14): each project's wiki and the skill library shared by every
// project. A wiki is an OKF bundle, a folder of markdown files in the hub's
// state directory whose history a local git repository keeps. The database
// holds none of it; an index in memory answers searches and listings.
package wiki

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Options configure a bundle.
type Options struct {
	// Layout is how the bundle is organised; the index files are
	// generated from it.
	Layout Layout
	// Git keeps the bundle's history in a git repository in its folder.
	Git bool
	// ReadOnly is for bundles mounted from elsewhere: they are indexed and
	// searched, never written.
	ReadOnly bool
	// Human is the actor ("human:<id>") changes made outside Veyloom, in an
	// editor say, are committed as. Without one they are indexed but left
	// uncommitted.
	Human string
	// Secrets screens every write; nil uses DefaultScanner.
	Secrets *Scanner
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Bundle is one OKF bundle on disk, indexed in memory.
type Bundle struct {
	dir  string
	opts Options
	git  *repo

	mu       sync.RWMutex
	pages    map[string]*entry        // by path, like "/decisions/x.md"
	problems map[string][]okf.Problem // files that break OKF, by path
	busy     map[string]int           // pages with writes not committed yet, by writer count
}

// process is the actor for what Veyloom does on its own.
var process = okf.Process("veyloom")

// maxPageSize bounds one page; a wiki page that big is a dump, not a page.
const maxPageSize = 256 << 10

// Open opens the bundle in dir, creating it (and its git repository) when
// it is not there yet. A writable bundle always has its root index with
// the OKF version, an index per directory of the layout, and a log.
func Open(ctx context.Context, dir string, opts Options) (*Bundle, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Secrets == nil {
		opts.Secrets = DefaultScanner()
	}
	b := &Bundle{dir: dir, opts: opts, pages: map[string]*entry{}, problems: map[string][]okf.Problem{}, busy: map[string]int{}}
	if opts.ReadOnly {
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("wiki: open %s: %w", dir, err)
		}
		return b, b.scan()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wiki: create %s: %w", dir, err)
	}
	if opts.Git {
		r, err := openRepo(ctx, dir)
		if err != nil {
			return nil, err
		}
		b.git = r
	}
	if err := b.scan(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fresh, err := b.setUp()
	if err != nil {
		return nil, err
	}
	if b.git != nil {
		if err := b.recordLeftovers(ctx, fresh); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Dir is the bundle's folder.
func (b *Bundle) Dir() string { return b.dir }

// KeepsHistory reports whether the bundle's changes go into git, so they
// can be looked back on and undone.
func (b *Bundle) KeepsHistory() bool { return b.git != nil }

// File is where the page at p is on disk.
func (b *Bundle) File(p string) (string, error) {
	p, err := CleanPath(p)
	if err != nil {
		return "", err
	}
	return b.file(p), nil
}

func (b *Bundle) now() time.Time { return b.opts.Now() }

// scan indexes every page in the folder, replacing what was indexed.
func (b *Bundle) scan() error {
	pages := map[string]*entry{}
	problems := map[string][]okf.Problem{}
	err := filepath.WalkDir(b.dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != b.dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		rel, _ := filepath.Rel(b.dir, fp)
		p := "/" + filepath.ToSlash(rel)
		e, probs, err := b.load(p)
		if err != nil {
			return err
		}
		if e != nil {
			pages[p] = e
		}
		if len(probs) > 0 {
			problems[p] = probs
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("wiki: read %s: %w", b.dir, err)
	}
	b.mu.Lock()
	b.pages, b.problems = pages, problems
	b.mu.Unlock()
	return nil
}

// load reads one file: a page, or for index.md and log.md only the
// problems they have.
func (b *Bundle) load(p string) (*entry, []okf.Problem, error) {
	fp := b.file(p)
	info, err := os.Stat(fp)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, nil, err
	}
	probs := okf.CheckFile(p, data, strings.Count(p, "/") == 1, okf.Conformance)
	if base := filepath.Base(fp); base == okf.IndexFile || base == okf.LogFile {
		return nil, probs, nil
	}
	e, err := newEntry(p, data, info)
	if err != nil {
		return nil, probs, nil
	}
	return e, probs, nil
}

func (b *Bundle) file(p string) string {
	return filepath.Join(b.dir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
}

// Page returns one page, with a copy of its document the caller may
// change and hand back to a Writer.
func (b *Bundle) Page(p string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	b.mu.RLock()
	e := b.pages[p]
	b.mu.RUnlock()
	if e == nil {
		return Page{}, fmt.Errorf("%w: no page %s", store.ErrNotFound, p)
	}
	return e.page()
}

// Pages lists every page, by path.
func (b *Bundle) Pages() []Summary {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Summary, 0, len(b.pages))
	for _, e := range b.pages {
		out = append(out, e.sum)
	}
	slices.SortFunc(out, func(x, y Summary) int { return strings.Compare(x.Path, y.Path) })
	return out
}

// Backlinks lists the pages that link to p, by path.
func (b *Bundle) Backlinks(p string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.backlinks(p)
}

func (b *Bundle) backlinks(p string) []string {
	var from []string
	for q, e := range b.pages {
		if q != p && slices.Contains(e.links, p) {
			from = append(from, q)
		}
	}
	slices.Sort(from)
	return from
}

// Problems lists what breaks OKF in the files as they are on disk, for
// bundles Veyloom did not write; its own writes are checked before they
// happen.
func (b *Bundle) Problems() []okf.Problem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []okf.Problem
	for _, probs := range b.problems {
		out = append(out, probs...)
	}
	slices.SortFunc(out, func(x, y okf.Problem) int { return strings.Compare(x.Path+x.Rule, y.Path+y.Rule) })
	return out
}

// writeFile replaces a file in one step, so a reader never sees half of it.
func writeFile(fp string, data []byte) error { return writeFileMode(fp, data, 0o644) }

// writeFileMode is writeFile giving the file mode.
func writeFileMode(fp string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fp), ".veyloom-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), fp)
}

// readFile reads a file of the bundle; a missing one reads as empty.
func (b *Bundle) readFile(p string) ([]byte, error) {
	data, err := os.ReadFile(b.file(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}
