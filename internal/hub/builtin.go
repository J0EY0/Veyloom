package hub

import (
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Veyloom's own skills (docs/design.md 5.23.6): every agent has them, in
// every project, apart from the skills people install for agents from the
// skill library. They come with Veyloom, in the module's skills/ folder,
// and are read once as the hub starts.

// teamPractices is the one of them saying how members split, hand on,
// check and report back work.
const teamPractices = "team-practices"

// BuiltinSkill is one of Veyloom's own skills as people see it: its name,
// and what its description says it is for.
type BuiltinSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// WithSkills gives the hub Veyloom's own skills: one folder each in fsys,
// as Agent Skills lays them out. Without it agents have only the skills
// installed for them.
func WithSkills(fsys fs.FS) Option {
	return func(h *Hub) { h.skills = fsys }
}

// BuiltinSkills lists Veyloom's own skills, by name.
func (h *Hub) BuiltinSkills() []BuiltinSkill {
	return slices.Clone(h.builtinSkills)
}

// readBuiltinSkills reads Veyloom's own skills out of fsys: each folder
// with a SKILL.md naming it and saying what it is for, with its text
// files. A folder that does not read as a skill is left out, and said so.
func readBuiltinSkills(fsys fs.FS, logger *slog.Logger) ([]runtime.Skill, []BuiltinSkill) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		logger.Error("read Veyloom's own skills", "err", err)
		return nil, nil
	}
	var skills []runtime.Skill
	var list []BuiltinSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		main, err := fs.ReadFile(fsys, path.Join(name, okf.SkillFile))
		if err != nil {
			logger.Error("one of Veyloom's own skills has no "+okf.SkillFile, "skill", name, "err", err)
			continue
		}
		d, err := okf.Parse(main)
		var description string
		if err == nil {
			description, _ = d.String(okf.KeyDescription)
		}
		if err != nil || d.Name() != name || strings.TrimSpace(description) == "" {
			logger.Error("one of Veyloom's own skills does not read as Agent Skills", "skill", name, "err", err)
			continue
		}
		files := map[string]string{}
		err = fs.WalkDir(fsys, name, func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return err
			}
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			// Text only: a runtime reads skills, and the set travels as JSON.
			if utf8.Valid(data) {
				files[strings.TrimPrefix(p, name+"/")] = string(data)
			}
			return nil
		})
		if err != nil {
			logger.Error("read one of Veyloom's own skills", "skill", name, "err", err)
			continue
		}
		skills = append(skills, runtime.Skill{Builtin: true, Name: name, Files: files})
		list = append(list, BuiltinSkill{Name: name, Description: strings.Join(strings.Fields(description), " ")})
	}
	return skills, list
}

// isBuiltin says name is one of Veyloom's own skills.
func (m *TurnManager) isBuiltin(name string) bool {
	return slices.ContainsFunc(m.builtin, func(s runtime.Skill) bool { return s.Name == name })
}
