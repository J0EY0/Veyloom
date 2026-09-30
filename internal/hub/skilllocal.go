package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Skills people keep for their runtimes on this machine (docs/design.md
// 5.15), for the library to take in with a tick rather than a typed path:
// their own, in the folders Claude Code, Codex and Pi load a person's
// skills from, and each project's, in its checkout. Veyloom runs where the
// runtimes do for now; once machines are elsewhere (design.md 5.20) the
// looking moves to them.
//
// These are the folders of every runtime at once, for a person to pick
// from; what each runtime does with a person's skill of the name of one
// it is given is runtime.ClashingSkillNames' and its likes' to say.

// LocalSkill is one of them.
type LocalSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Folder is where it is; Where says so for a person: a folder under the
	// home directory as ~/…, a project's as its name and the folder in its
	// checkout.
	Folder string `json:"folder"`
	Where  string `json:"where"`
	// InLibrary is set when the library has a skill of the name already.
	InLibrary bool `json:"in_library,omitempty"`
	// Origin is set when the library's skill of the name came from this
	// folder, by import or update: "changed" when the folder holds
	// something else now, "same" when not, "unknown" when it came in before
	// the library kept track. EditsSince counts the changes made to the
	// library's copy since it came in, which an update would replace.
	Origin     string `json:"origin,omitempty"`
	EditsSince int    `json:"edits_since,omitempty"`
	// Problem is why it cannot come in, by the code the import would refuse
	// it with and that code's params; empty when it can.
	Problem       string       `json:"problem,omitempty"`
	ProblemParams store.Params `json:"problem_params,omitempty"`
}

// The folders a person's skills are in, under the home directory and in a
// project's checkout.
var (
	personalSkillDirs = []string{".claude/skills", ".agents/skills", ".codex/skills", ".pi/agent/skills"}
	projectSkillDirs  = []string{".claude/skills", ".agents/skills", ".codex/skills", ".pi/skills"}
)

// LocalSkills lists the skills on this machine, in the order of the
// folders above and by name in each. A skill reached from two folders, as
// when ~/.claude/skills links to one in ~/.agents/skills, is listed once,
// where it was found first.
func (h *Hub) LocalSkills(ctx context.Context) ([]LocalSkill, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return nil, err
	}
	type place struct{ dir, where string }
	var places []place
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range personalSkillDirs {
			places = append(places, place{filepath.Join(home, filepath.FromSlash(d)), "~/" + d})
		}
	}
	projects, err := h.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.RepoPath == "" {
			continue
		}
		for _, d := range projectSkillDirs {
			places = append(places, place{filepath.Join(p.RepoPath, filepath.FromSlash(d)), p.Name + " · " + d})
		}
	}

	out := []LocalSkill{}
	seen := map[string]bool{}
	for _, at := range places {
		entries, err := os.ReadDir(at.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			folder := filepath.Join(at.dir, e.Name())
			real, err := filepath.EvalSymlinks(folder)
			if err != nil || seen[real] || !isDir(real) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(real, okf.SkillFile))
			if err != nil {
				continue
			}
			seen[real] = true
			s, files := h.localSkill(r, e.Name(), folder, at.where, data)
			if s.InLibrary && s.Problem == "" {
				h.localOrigin(ctx, r, &s, real, files)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// localOrigin says whether the library's skill of the name came from the
// folder of s, which really is at real and holds files, and whether the
// folder changed since.
func (h *Hub) localOrigin(ctx context.Context, r wikiRef, s *LocalSkill, real string, files []string) {
	origin, err := skillOriginOf(ctx, r, s.Name)
	if err != nil || origin.folder == "" {
		return
	}
	from := filepath.Clean(origin.folder)
	if resolved, err := filepath.EvalSymlinks(from); err == nil {
		from = resolved
	}
	if from != real {
		return
	}
	s.EditsSince = origin.since
	switch hash, err := skillFolderHash(real, files); {
	case origin.hash == "" || err != nil:
		s.Origin = "unknown"
	case hash == origin.hash:
		s.Origin = "same"
	default:
		s.Origin = "changed"
	}
}

// skillOrigin is where a skill of the library came from: the folder of
// this machine it was last imported or updated from, "" when it came from
// an upload or cannot be told, and what that folder held then, "" when it
// came in before the library kept track. since counts the commits that
// changed the skill after it came in.
type skillOrigin struct {
	folder, hash string
	since        int
}

// skillOriginOf reads where the library's skill called name came from out
// of its history.
func skillOriginOf(ctx context.Context, r wikiRef, name string) (skillOrigin, error) {
	commits, err := r.bundle.SkillHistory(ctx, name, 500)
	if err != nil {
		return skillOrigin{}, err
	}
	imported := "Imported the skill " + name + " from "
	since := 0
	for _, c := range commits {
		if folder := c.Trailers[trailerSource]; folder != "" {
			return skillOrigin{folder: folder, hash: c.Trailers[trailerSourceHash], since: since}, nil
		}
		// Imported before the history named it apart: the subject does.
		if rest, ok := strings.CutPrefix(c.Subject, imported); ok {
			if !filepath.IsAbs(rest) {
				rest = ""
			}
			return skillOrigin{folder: rest, since: since}, nil
		}
		if !recordOnly(c.Subject) {
			since++
		}
	}
	return skillOrigin{}, nil
}

// recordOnly reports whether a commit of the library, by its subject, only
// kept a record of a skill, which an update keeps too: a confirmation, a
// trial kept, a team handed over or gone, being taken out of use or put
// back. Its text stayed as it was.
func recordOnly(subject string) bool {
	for _, prefix := range []string{"Confirmed: ", "Handed over to ", "Retired: ", "Put back in use: ", "Left without a team: "} {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return strings.HasPrefix(subject, "Keep ") && strings.HasSuffix(subject, " after its trial")
}

// localSkill reads what a skill's SKILL.md says of it, and whether the
// import would take it; with the files the import would take, when it
// would.
func (h *Hub) localSkill(r wikiRef, dirName, folder, where string, data []byte) (LocalSkill, []string) {
	s := LocalSkill{Name: dirName, Folder: folder, Where: where}
	d, err := okf.Parse(data)
	if err != nil {
		s.Problem = "skillUnreadable"
		return s, nil
	}
	var files []string
	if name, _ := d.String(okf.KeyName); name != "" {
		s.Name = name
	}
	desc, _ := d.String(okf.KeyDescription)
	s.Description = strings.Join(strings.Fields(desc), " ")
	switch {
	case !wikiSlug(s.Name):
		s.Problem = "skillBadName"
	case h.turns.isBuiltin(s.Name):
		s.Problem = "skillBuiltin"
	case s.Description == "":
		s.Problem = "skillNoDescription"
	case d.Type() != "" && d.Type() != "Skill":
		s.Problem = "skillUnreadable"
	default:
		// Its files, as many and as big as a turn can carry.
		var p *store.Problem
		if files, err = importFilesOf(folder); errors.As(err, &p) {
			s.Problem, s.ProblemParams = p.Code, p.Params
		} else if err != nil {
			s.Problem = "skillUnreadable"
		}
	}
	if wikiSlug(s.Name) {
		if _, err := r.bundle.Page(wiki.SkillPath(s.Name)); err == nil {
			s.InLibrary = true
		}
	}
	return s, files
}
