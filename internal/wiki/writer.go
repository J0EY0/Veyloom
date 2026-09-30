package wiki

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Writer groups the writes of one turn, one maintainer run or one change
// in the web UI, so they go into the history as one commit by one author
// (design.md 5.5). Each write lands on disk and in the index at once, so
// what is written can be read back straight away; Commit records them.
type Writer struct {
	b       *Bundle
	author  string
	order   []string // pages in the order first touched
	changes map[string]*change
}

// change is what a writer did to one page, for the log.
type change struct {
	existed     bool // the page was there before the writer touched it
	content     bool // its content changed
	deprecated  bool
	verified    bool
	renamedFrom string
	gone        bool // renamed away; the old path is in the commit
	// note is said after the page's line in the log, why say.
	note string
}

// Writer starts a group of writes by author, an OKF actor.
func (b *Bundle) Writer(author string) (*Writer, error) {
	if b.opts.ReadOnly {
		return nil, fmt.Errorf("%w: %s is mounted read-only", store.ErrInvalidInput, b.dir)
	}
	if !okf.ValidActor(author) {
		return nil, fmt.Errorf("%w: %q is not an actor (human:<id>, process:<id> or <producer>/<version>)", store.ErrInvalidInput, author)
	}
	return &Writer{b: b, author: author, changes: map[string]*change{}}, nil
}

// Pending lists the pages written and not committed yet.
func (w *Writer) Pending() []string { return append([]string(nil), w.order...) }

// InvalidError refuses a write that does not follow OKF v0.2 the way
// Veyloom writes it (design.md 5.13).
type InvalidError struct {
	Path     string
	Problems []okf.Problem
}

func (e *InvalidError) Error() string {
	msgs := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		msgs = append(msgs, p.Message)
	}
	return fmt.Sprintf("%s does not follow OKF v0.2 as Veyloom writes it: %s", e.Path, strings.Join(msgs, "; "))
}

func (e *InvalidError) Unwrap() error { return store.ErrInvalidInput }

// Create writes a new page at p.
func (w *Writer) Create(p string, d *okf.Document) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	if err := checkNewPath(p); err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	if w.b.pages[p] != nil {
		return Page{}, fmt.Errorf("%w: %s is already there", store.ErrConflict, p)
	}
	if _, err := os.Lstat(w.b.file(p)); err == nil {
		return Page{}, fmt.Errorf("%w: %s is already there", store.ErrConflict, p)
	}
	return w.save(p, d.Clone(), authored, func(c *change) { c.content = true })
}

// Put replaces the page at p with d. hash is the Page.Hash it was read
// with; if the page changed since, nothing is written.
func (w *Writer) Put(p string, d *okf.Document, hash string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	if _, err := w.current(p, hash); err != nil {
		return Page{}, err
	}
	return w.save(p, d.Clone(), authored, func(c *change) { c.content = true })
}

// Edited returns the page at p as edits would leave it, without writing
// anything, and the hash of the page they were applied to: what a change
// that waits for a person is made of.
func (b *Bundle) Edited(p string, edits []Edit) (*okf.Document, string, error) {
	p, err := CleanPath(p)
	if err != nil {
		return nil, "", err
	}
	b.mu.RLock()
	e := b.pages[p]
	b.mu.RUnlock()
	if e == nil {
		return nil, "", fmt.Errorf("%w: no page %s", store.ErrNotFound, p)
	}
	text, err := applyEdits(string(e.data), edits)
	if err != nil {
		return nil, "", err
	}
	d, err := okf.Parse([]byte(text))
	if err != nil {
		return nil, "", fmt.Errorf("%w: after the edits %s no longer parses: %v", store.ErrInvalidInput, p, err)
	}
	return d, e.hash, nil
}

// Edit applies edits to the page's text, frontmatter included, the way
// agents patch pages. With a hash, the page must not have changed since
// it was read; without one, the edits' own targets guard against that.
func (w *Writer) Edit(p string, edits []Edit, hash string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, hash)
	if err != nil {
		return Page{}, err
	}
	text, err := applyEdits(string(e.data), edits)
	if err != nil {
		return Page{}, err
	}
	d, err := okf.Parse([]byte(text))
	if err != nil {
		return Page{}, fmt.Errorf("%w: after the edits %s no longer parses: %v", store.ErrInvalidInput, p, err)
	}
	return w.save(p, d, authored, func(c *change) { c.content = true })
}

// Verify records that the writer's author confirmed the page as it is
// (OKF §5.2). The content and its generated stamp stay as they are.
func (w *Writer) Verify(p string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	d.AddVerified(okf.Stamp{By: w.author, At: w.b.now()})
	return w.save(p, d, mechanical, func(c *change) { c.verified = true })
}

// Note adds words to the log's line for a page the writer has written,
// after what happened: why, or on what grounds.
func (w *Writer) Note(p, text string) error {
	p, err := CleanPath(p)
	if err != nil {
		return err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	c := w.changes[p]
	if c == nil {
		return fmt.Errorf("%w: %s was not written", store.ErrInvalidInput, p)
	}
	c.note = strings.Join(strings.Fields(text), " ")
	return nil
}

// Tag puts a tag on the page or takes it off. It classifies the page, it
// does not rewrite it: the content and the stamp of who generated it stay
// as they are. A page that has the tag already, or has not, is left alone.
func (w *Writer) Tag(p, tag string, on bool) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	if tag = strings.TrimSpace(tag); tag == "" {
		return Page{}, fmt.Errorf("%w: a tag needs a name", store.ErrInvalidInput)
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	if e.sum.Tagged(tag) == on {
		return e.page()
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	tags := slices.DeleteFunc(d.Tags(), func(t string) bool { return strings.EqualFold(t, tag) })
	if on {
		tags = append(tags, tag)
	}
	d.SetTags(tags)
	return w.save(p, d, mechanical, func(c *change) { c.content = true })
}

// Deprecate marks the page as no longer current (OKF §5.4). With a
// successor, each page links to the other, the way OKF's own sample
// retires a definition; the old page stays for links and history.
func (w *Writer) Deprecate(p, successor, reason string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	if e.sum.Status == okf.Deprecated {
		return Page{}, fmt.Errorf("%w: %s is already deprecated", store.ErrConflict, p)
	}
	var next *entry
	if successor != "" {
		if successor, err = CleanPath(successor); err != nil {
			return Page{}, err
		}
		if next = w.b.pages[successor]; next == nil || successor == p || next.sum.Status == okf.Deprecated {
			return Page{}, fmt.Errorf("%w: %s cannot take over from %s", store.ErrInvalidInput, successor, p)
		}
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	note := "> **Deprecated**"
	if reason = strings.Join(strings.Fields(reason), " "); reason != "" {
		note += ": " + reason
	}
	if next != nil {
		note += "\n>\n> Superseded by [" + linkText(next.sum.Title) + "](" + successor + ")."
	}
	d.SetStatus(okf.Deprecated)
	d.SetBody(note + "\n\n" + d.Body())
	page, err := w.save(p, d, authored, func(c *change) { c.deprecated = true })
	if err != nil || next == nil || containsLink(next, p) {
		return page, err
	}
	nd, err := okf.Parse(next.data)
	if err != nil {
		return Page{}, err
	}
	nd.SetBody(strings.TrimRight(nd.Body(), "\n") + "\n\n> Supersedes [" + linkText(e.sum.Title) + "](" + p + ").\n")
	if _, err := w.save(successor, nd, authored, func(c *change) { c.content = true }); err != nil {
		return Page{}, err
	}
	return page, nil
}

// Rename moves a page and points every link to it, and every source naming
// it, at the new path.
func (w *Writer) Rename(from, to string) (Page, error) {
	from, err := CleanPath(from)
	if err != nil {
		return Page{}, err
	}
	if to, err = CleanPath(to); err != nil {
		return Page{}, err
	}
	if err := checkNewPath(to); err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(from, "")
	if err != nil {
		return Page{}, err
	}
	if w.b.pages[to] != nil {
		return Page{}, fmt.Errorf("%w: %s is already there", store.ErrConflict, to)
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	d.SetBody(rewriteLinks(from, to, from, d.Body()))
	// Work out every rewrite before writing anything.
	linking := map[string]*okf.Document{}
	for _, q := range w.b.backlinks(from) {
		qd, err := okf.Parse(w.b.pages[q].data)
		if err != nil {
			continue
		}
		qd.SetBody(rewriteLinks(q, to, from, qd.Body()))
		qd.ReplaceSourceResource(from, to)
		linking[q] = qd
	}
	page, err := w.save(to, d, mechanical, func(c *change) { c.renamedFrom = from })
	if err != nil {
		return Page{}, err
	}
	if err := os.Remove(w.b.file(from)); err != nil {
		return Page{}, err
	}
	delete(w.b.pages, from)
	w.touch(from).gone = true
	for q, qd := range linking {
		if _, err := w.save(q, qd, mechanical, func(*change) {}); err != nil {
			return Page{}, err
		}
	}
	return page, w.b.regenerate(from)
}

// check holds an authored page to what Veyloom writes.
func (w *Writer) check(p string, d *okf.Document, data []byte) error {
	if len(data) > maxPageSize {
		return tooBig(p, len(data))
	}
	if problems := okf.CheckConcept(p, d, okf.Strict); len(problems) > 0 {
		return &InvalidError{Path: p, Problems: problems}
	}
	if found := w.b.opts.Secrets.Scan(string(data)); len(found) > 0 {
		return &SecretError{Path: p, Findings: found}
	}
	return nil
}

// current is the page at p, which must be there and, given a hash, must
// not have changed since it was read.
func (w *Writer) current(p, hash string) (*entry, error) {
	e := w.b.pages[p]
	if e == nil {
		return nil, fmt.Errorf("%w: no page %s", store.ErrNotFound, p)
	}
	if hash != "" && hash != e.hash {
		return nil, fmt.Errorf("%w: %s changed since it was read; read it again", store.ErrConflict, p)
	}
	return e, nil
}

// authored is a write of content that is the writer's own: it is stamped
// as generated by the writer's author, held to the strict profile and
// screened for secrets. The other writes (a confirmation, links pointed at
// a renamed page) leave the content as it was, whoever wrote it.
const (
	authored   = true
	mechanical = false
)

// save writes one page in one step and indexes it; an authored write is
// checked first.
func (w *Writer) save(p string, d *okf.Document, own bool, note func(*change)) (Page, error) {
	if own {
		d.SetGenerated(okf.Stamp{By: w.author, At: w.b.now()})
	}
	data, err := d.Bytes()
	if err != nil {
		return Page{}, err
	}
	if own {
		if err := w.check(p, d, data); err != nil {
			return Page{}, err
		}
	} else if len(data) > maxPageSize {
		return Page{}, tooBig(p, len(data))
	}
	c := w.touch(p)
	if err := writeFile(w.b.file(p), data); err != nil {
		return Page{}, err
	}
	info, err := os.Stat(w.b.file(p))
	if err != nil {
		return Page{}, err
	}
	e, err := newEntry(p, data, info)
	if err != nil {
		return Page{}, err
	}
	w.b.pages[p] = e
	delete(w.b.problems, p)
	note(c)
	if err := w.b.regenerate(p); err != nil {
		return Page{}, err
	}
	return e.page()
}

// PutFile writes a file of a skill's folder that is not a page: a script,
// a template, whatever the skill's instructions point at (design.md 5.9:
// they go in as they are). Pages go through Create and Put, which hold
// them to OKF. Hidden files and ones too big for a runtime to read are
// refused.
func (w *Writer) PutFile(p string, data []byte) error {
	p = path.Clean("/" + p)
	name := SkillOfFile(p)
	switch {
	case name == "" || !slug.MatchString(name) || len(name) > 64:
		return fmt.Errorf("%w: %s is in no skill's folder", store.ErrInvalidInput, p)
	case path.Ext(p) == ".md":
		return fmt.Errorf("%w: %s is a page: write it as one", store.ErrInvalidInput, p)
	case strings.Contains(p, "/."):
		return fmt.Errorf("%w: %s has a hidden part", store.ErrInvalidInput, p)
	case len(data) > MaxSkillFile:
		return fmt.Errorf("%w: %s is %s MB; a skill's file should stay under %s MB", store.ErrInvalidInput, p, MB(int64(len(data))), MB(MaxSkillFile))
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	w.touch(p)
	return writeFile(w.b.file(p), data)
}

// touch returns what the writer did to p, starting the record on first use.
func (w *Writer) touch(p string) *change {
	if c := w.changes[p]; c != nil {
		return c
	}
	c := &change{existed: w.b.pages[p] != nil}
	w.changes[p] = c
	w.order = append(w.order, p)
	w.b.busy[p]++
	return c
}

// Trailer is a "Key: value" line at the end of a commit message, naming
// the turn, member or project a commit came from.
type Trailer struct {
	Key, Value string
}

// Commit records the writer's pages as one commit, with a line for each in
// the bundle's log, and returns the commit (empty without git). The writer
// can be used again afterwards.
func (w *Writer) Commit(ctx context.Context, subject string, trailers ...Trailer) (string, error) {
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	if len(w.order) == 0 {
		return "", nil
	}
	defer w.reset()
	entries := w.logEntries()
	if err := w.b.appendLog(entries); err != nil {
		return "", err
	}
	if w.b.git == nil {
		return "", nil
	}
	files := append(append([]string(nil), w.order...), "/"+okf.LogFile)
	files = append(files, indexFiles(w.order...)...)
	return w.b.git.commit(ctx, files, w.author, message(subject, entries, trailers))
}

func (w *Writer) reset() {
	for _, p := range w.order {
		if w.b.busy[p]--; w.b.busy[p] <= 0 {
			delete(w.b.busy, p)
		}
	}
	w.order, w.changes = nil, map[string]*change{}
}

// logEntries turns what the writer did into log lines, one per page.
func (w *Writer) logEntries() []okf.LogEntry {
	var entries []okf.LogEntry
	for _, p := range w.order {
		c := w.changes[p]
		e := w.b.pages[p]
		if c.gone || e == nil {
			continue
		}
		link := "[" + linkText(e.sum.Title) + "](" + p + ")"
		by := " by " + w.author
		if c.note != "" {
			by += ": " + c.note
		}
		switch {
		case c.renamedFrom != "":
			entries = append(entries, okf.LogEntry{Kind: okf.LogRename, Text: link + " was " + c.renamedFrom + by})
		case c.deprecated:
			entries = append(entries, okf.LogEntry{Kind: okf.LogDeprecation, Text: link + by})
		case !c.existed:
			entries = append(entries, okf.LogEntry{Kind: okf.LogCreation, Text: link + by})
		case c.content:
			entries = append(entries, okf.LogEntry{Kind: okf.LogUpdate, Text: link + by})
		case c.verified:
			entries = append(entries, okf.LogEntry{Kind: okf.LogVerification, Text: link + by})
		}
	}
	return entries
}

func (b *Bundle) appendLog(entries []okf.LogEntry) error {
	log, err := b.readFile("/" + okf.LogFile)
	if err != nil {
		return err
	}
	return writeFile(b.file("/"+okf.LogFile), okf.AppendLog(log, b.today(), entries))
}

// message is a commit message: the subject, the log lines, the trailers.
func message(subject string, entries []okf.LogEntry, trailers []Trailer) string {
	var b strings.Builder
	b.WriteString(strings.Join(strings.Fields(subject), " "))
	if len(entries) > 0 {
		b.WriteString("\n\n")
		for _, e := range entries {
			b.WriteString("* " + e.Kind + ": " + e.Text + "\n")
		}
	}
	if len(trailers) > 0 {
		b.WriteString("\n")
		for _, t := range trailers {
			b.WriteString(t.Key + ": " + strings.Join(strings.Fields(t.Value), " ") + "\n")
		}
	}
	return b.String()
}

// rewriteLinks points links written in the page at at, which resolve to
// from, at to.
func rewriteLinks(at, to, from, body string) string {
	return okf.RewriteLinks(body, func(target string) (string, bool) {
		p, ok := okf.Resolve(at, target)
		return to, ok && p == from
	})
}

func containsLink(e *entry, p string) bool {
	for _, l := range e.links {
		if l == p {
			return true
		}
	}
	return false
}

func linkText(s string) string {
	return strings.NewReplacer(`[`, `\[`, `]`, `\]`).Replace(s)
}

// tooBig is what a page over maxPageSize is refused with.
func tooBig(p string, size int) error {
	return store.Invalid("pageTooBig", store.Params{"path": p, "kb": strconv.Itoa(size >> 10), "max": strconv.Itoa(maxPageSize >> 10)},
		"%s is %d KB; a page should stay under %d KB", p, size>>10, maxPageSize>>10)
}
