package hub

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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
	// Problem is why it cannot come in, by the code the import would refuse
	// it with; empty when it can.
	Problem string `json:"problem,omitempty"`
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
			out = append(out, h.localSkill(r, e.Name(), folder, at.where, data))
		}
	}
	return out, nil
}

// localSkill reads what a skill's SKILL.md says of it, and whether the
// import would take it.
func (h *Hub) localSkill(r wikiRef, dirName, folder, where string, data []byte) LocalSkill {
	s := LocalSkill{Name: dirName, Folder: folder, Where: where}
	d, err := okf.Parse(data)
	if err != nil {
		s.Problem = "skillUnreadable"
		return s
	}
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
	}
	if wikiSlug(s.Name) {
		if _, err := r.bundle.Page(wiki.SkillPath(s.Name)); err == nil {
			s.InLibrary = true
		}
	}
	return s
}
