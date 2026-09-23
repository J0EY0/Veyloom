package wiki

import (
	"bytes"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Layout is how a bundle is organised: the top-level directories Veyloom
// keeps in it, and what goes where. Index files are generated from it.
type Layout struct {
	Dirs     []Dir
	LogTitle string
}

// Dir is one top-level directory of a layout.
type Dir struct {
	Name        string
	Type        string // the type of the pages its index lists; "" lists all
	Heading     string
	Description string
}

// Generated scaffolding is written in English, like OKF's own examples;
// titles and descriptions are the pages' own.

// ProjectLayout is a project wiki's layout (design.md 5.3).
var ProjectLayout = Layout{
	LogTitle: "Project wiki history",
	Dirs: []Dir{
		{"decisions", "Decision", "Decisions", "Choices that were made, with the reasons at the time."},
		{"conventions", "Convention", "Conventions", "Agreed ways of doing things."},
		{"facts", "Fact", "Facts", "Things checked to be true: versions, behaviour, how interfaces really work."},
		{"pitfalls", "Pitfall", "Pitfalls", "Traps hit before, and the way around them."},
		{"modules", "Module", "Modules", "What each module or directory is for, and where its edges are."},
		{"topics", "Topic", "Topics", "What finished topics came to: the question, the outcome, the files, the lessons."},
	},
}

// LibraryLayout is the skill library's layout (design.md 5.10): the
// skills first, what the library is for, then the patterns they are made
// of.
var LibraryLayout = Layout{
	LogTitle: "Skill library history",
	Dirs: []Dir{
		{"skills", "Skill", "Skills", "Skills the runtimes load, each owned by a project."},
		{"patterns", "Pattern", "Patterns", "How agents fail or succeed at tasks, with root causes and fixes."},
	},
}

func (l Layout) dir(name string) (Dir, bool) {
	i := slices.IndexFunc(l.Dirs, func(d Dir) bool { return d.Name == name })
	if i < 0 {
		return Dir{Name: name, Heading: name}, false
	}
	return l.Dirs[i], true
}

// setUp gives a writable bundle what every bundle Veyloom keeps has: a log
// and the generated indexes. It reports whether the bundle was new.
func (b *Bundle) setUp() (bool, error) {
	log, err := b.readFile("/" + okf.LogFile)
	if err != nil {
		return false, err
	}
	fresh := log == nil
	if fresh {
		title := b.opts.Layout.LogTitle
		if title == "" {
			title = "History"
		}
		log = okf.AppendLog(okf.NewLog(title), b.today(), []okf.LogEntry{{Kind: okf.LogInitialization, Text: "Set up the bundle."}})
		if err := writeFile(b.file("/"+okf.LogFile), log); err != nil {
			return false, err
		}
	}
	return fresh, b.regenerateAll()
}

func (b *Bundle) today() string { return b.now().Local().Format("2006-01-02") }

// indexFiles are the index files that listing the given pages touches:
// the root's and each top-level directory's.
func indexFiles(pages ...string) []string {
	files := []string{"/" + okf.IndexFile}
	for _, p := range pages {
		if d := topDir(p); d != "" && !slices.Contains(files, "/"+d+"/"+okf.IndexFile) {
			files = append(files, "/"+d+"/"+okf.IndexFile)
		}
	}
	return files
}

// generated reports whether Veyloom writes a file itself: the log, and the
// indexes of the root and the top-level directories.
func generated(p string) bool {
	return p == "/"+okf.LogFile || p == "/"+okf.IndexFile || (strings.Count(p, "/") == 2 && strings.HasSuffix(p, "/"+okf.IndexFile))
}

// regenerate rewrites the indexes listing the given pages.
func (b *Bundle) regenerate(pages ...string) error {
	for _, f := range indexFiles(pages...) {
		if err := b.writeIndex(f); err != nil {
			return err
		}
	}
	return nil
}

// regenerateAll rewrites every generated index: the root's, each layout
// directory's, and each other top-level directory with pages in it.
func (b *Bundle) regenerateAll() error {
	for _, f := range b.indexes() {
		if err := b.writeIndex(f); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bundle) writeIndex(f string) error {
	var data []byte
	if f == "/"+okf.IndexFile {
		data = okf.RenderIndex(true, b.rootSections())
	} else {
		data = okf.RenderIndex(false, b.dirSections(topDir(f)))
	}
	old, err := b.readFile(f)
	if err != nil || bytes.Equal(old, data) {
		return err
	}
	return writeFile(b.file(f), data)
}

func (b *Bundle) rootSections() []okf.IndexSection {
	dirs := okf.IndexSection{Heading: "Directories"}
	listed := map[string]bool{}
	for _, d := range b.opts.Layout.Dirs {
		dirs.Entries = append(dirs.Entries, okf.IndexEntry{Title: d.Name, Link: "/" + d.Name + "/" + okf.IndexFile, Description: d.Description})
		listed[d.Name] = true
	}
	var others []string
	pages := okf.IndexSection{Heading: "Pages"}
	for _, s := range b.sortedPages() {
		switch d := topDir(s.Path); {
		case d == "":
			pages.Entries = append(pages.Entries, indexEntry(s))
		case !listed[d]:
			listed[d] = true
			others = append(others, d)
		}
	}
	slices.Sort(others)
	for _, d := range others {
		dirs.Entries = append(dirs.Entries, okf.IndexEntry{Title: d, Link: "/" + d + "/" + okf.IndexFile})
	}
	return nonEmpty(dirs, pages)
}

// dirSections lists a top-level directory's pages of its type, the current
// ones first, then drafts, then deprecated ones. Current skills are listed
// by the team that owns them (design.md 5.10), the unowned last.
func (b *Bundle) dirSections(name string) []okf.IndexSection {
	d, _ := b.opts.Layout.dir(name)
	current := okf.IndexSection{Heading: d.Heading}
	drafts := okf.IndexSection{Heading: "Drafts"}
	deprecated := okf.IndexSection{Heading: "Deprecated"}
	byTeam := map[string]*okf.IndexSection{}
	for _, s := range b.sortedPages() {
		if topDir(s.Path) != name || (d.Type != "" && s.Type != d.Type) {
			continue
		}
		switch {
		case s.Status == okf.Draft:
			drafts.Entries = append(drafts.Entries, indexEntry(s))
		case s.Status == okf.Deprecated:
			deprecated.Entries = append(deprecated.Entries, indexEntry(s))
		case d.Type == "Skill":
			team := byTeam[s.Team]
			if team == nil {
				heading := "Owned by " + s.Team
				if s.Team == "" {
					heading = "Owned by no team"
				}
				team = &okf.IndexSection{Heading: heading}
				byTeam[s.Team] = team
			}
			team.Entries = append(team.Entries, indexEntry(s))
		default:
			current.Entries = append(current.Entries, indexEntry(s))
		}
	}
	sections := []okf.IndexSection{current}
	if d.Type == "Skill" && len(byTeam) > 0 {
		teams := make([]string, 0, len(byTeam))
		for team := range byTeam {
			teams = append(teams, team)
		}
		// By name, and nobody's last.
		slices.SortFunc(teams, func(x, y string) int {
			switch {
			case x == "":
				return 1
			case y == "":
				return -1
			}
			return strings.Compare(x, y)
		})
		sections = sections[:0]
		for _, team := range teams {
			sections = append(sections, *byTeam[team])
		}
	}
	for _, s := range []okf.IndexSection{drafts, deprecated} {
		if len(s.Entries) > 0 {
			sections = append(sections, s)
		}
	}
	return sections
}

func (b *Bundle) sortedPages() []Summary {
	out := make([]Summary, 0, len(b.pages))
	for _, e := range b.pages {
		out = append(out, e.sum)
	}
	slices.SortFunc(out, func(x, y Summary) int { return strings.Compare(x.Path, y.Path) })
	return out
}

func indexEntry(s Summary) okf.IndexEntry {
	return okf.IndexEntry{Title: s.Title, Link: s.Path, Description: s.Description}
}

func nonEmpty(sections ...okf.IndexSection) []okf.IndexSection {
	var out []okf.IndexSection
	for _, s := range sections {
		if len(s.Entries) > 0 {
			out = append(out, s)
		}
	}
	return out
}
