package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Skills from Veyloom's skill library (docs/design.md 5.10, 5.11): with
// every turn the hub sends the skills its runtime may load, as Agent
// Skills has them, and the machine writes them where the runtime loads
// them from. Nothing is written to the person's own skill folders
// (~/.claude/skills, ~/.agents/skills); their skills load as always.

// SkillPlugin is the name of the Claude Code plugin the skills come in:
// Claude Code calls them veyloom:<name>.
const SkillPlugin = "veyloom"

// SkillSet is the skills a turn is given.
type SkillSet struct {
	// Hash names the set by its content: a machine that has written it
	// before does not write it again.
	Hash   string  `json:"hash"`
	Skills []Skill `json:"skills"`
}

// Names lists the skills of the set by name; none for a nil set.
func (s *SkillSet) Names() []string {
	return s.names(func(Skill) bool { return true })
}

// LibraryNames lists the set's skills installed from the library, and
// BuiltinNames Veyloom's own, which every agent has (design.md 5.23.6).
func (s *SkillSet) LibraryNames() []string {
	return s.names(func(skill Skill) bool { return !skill.Builtin })
}

func (s *SkillSet) BuiltinNames() []string {
	return s.names(func(skill Skill) bool { return skill.Builtin })
}

func (s *SkillSet) names(keep func(Skill) bool) []string {
	if s == nil {
		return nil
	}
	var names []string
	for _, skill := range s.Skills {
		if keep(skill) {
			names = append(names, skill.Name)
		}
	}
	return names
}

// Skill is one skill: its files by their path in its directory, SKILL.md
// among them. Builtin marks one of Veyloom's own, which every agent has
// (design.md 5.23.6), apart from those installed from the library.
type Skill struct {
	Builtin bool   `json:"builtin,omitempty"`
	Name    string `json:"name"`
	// Files are its text files, by their path in its folder, SKILL.md
	// among them; Blobs the rest, fonts, images, archives, as they are.
	Files map[string]string `json:"files,omitempty"`
	Blobs map[string][]byte `json:"blobs,omitempty"`
}

// keptSkillSets is how many sets a machine keeps written: the one a turn
// starts with, and a few before it for turns still running on them.
const keptSkillSets = 16

// WriteSkills writes a skill set under root, in a directory named after
// its hash, once, and returns that directory. It is laid out for every
// runtime at once: a Claude Code plugin (.claude-plugin/plugin.json), whose
// skills/ is a folder of skill folders Codex takes as a root and Pi one
// skill at a time. Sets written long ago are cleared away.
//
// A skill whose name taken holds, a person's own skill's (see
// ClashingSkillNames), goes by SkillAlias instead: Pi would keep the
// person's and leave the set's out. The directory's name then carries the
// renames too.
func WriteSkills(root string, set *SkillSet, taken map[string]bool) (string, error) {
	if set == nil || len(set.Skills) == 0 {
		return "", nil
	}
	if !isSkillHash(set.Hash) {
		return "", fmt.Errorf("skills: %q is no hash", set.Hash)
	}
	aliases := SkillAliases(set, taken)
	dir := filepath.Join(root, set.Hash+aliasSuffix(aliases))
	if _, err := os.Stat(dir); err == nil {
		now := time.Now()
		_ = os.Chtimes(dir, now, now)
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".writing-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	manifest, _ := json.MarshalIndent(map[string]string{
		"name":        SkillPlugin,
		"description": "Skills Veyloom gives this turn: its own, which every agent has, and those installed from its skill library.",
		"version":     "1.0.0",
	}, "", "  ")
	files := map[string][]byte{".claude-plugin/plugin.json": append(manifest, '\n')}
	for _, s := range set.Skills {
		if !isSkillName(s.Name) {
			return "", fmt.Errorf("skills: %q is no skill name", s.Name)
		}
		if _, ok := s.Files["SKILL.md"]; !ok {
			return "", fmt.Errorf("skills: %s has no SKILL.md", s.Name)
		}
		name := s.Name
		if alias, ok := aliases[s.Name]; ok {
			name = alias
		}
		for rel, content := range s.Files {
			if !inFolder(rel) {
				return "", fmt.Errorf("skills: %s has a file outside its folder: %q", s.Name, rel)
			}
			if rel == "SKILL.md" && name != s.Name {
				content = renameSkill(content, s.Name, name)
			}
			files["skills/"+name+"/"+rel] = []byte(content)
		}
		for rel, content := range s.Blobs {
			if !inFolder(rel) || rel == "SKILL.md" {
				return "", fmt.Errorf("skills: %s has a file outside its folder: %q", s.Name, rel)
			}
			files["skills/"+name+"/"+rel] = content
		}
	}
	for rel, content := range files {
		path := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		mode := fs.FileMode(0o644)
		if bytes.HasPrefix(content, []byte("#!")) {
			mode = 0o755
		}
		if err := os.WriteFile(path, content, mode); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another turn wrote the same set first.
		if _, statErr := os.Stat(dir); statErr == nil {
			return dir, nil
		}
		return "", err
	}
	clearOldSkillSets(root, dir)
	return dir, nil
}

// inFolder reports whether rel is a path within a skill's folder.
func inFolder(rel string) bool {
	clean := filepath.ToSlash(filepath.Clean(rel))
	return clean == rel && !filepath.IsAbs(rel) && !strings.HasPrefix(clean, "../") && clean != ".."
}

// clearOldSkillSets removes all but the latest sets under root.
func clearOldSkillSets(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type set struct {
		path string
		at   time.Time
	}
	var sets []set
	for _, e := range entries {
		if !e.IsDir() || !isSkillHash(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			sets = append(sets, set{filepath.Join(root, e.Name()), info.ModTime()})
		}
	}
	if len(sets) <= keptSkillSets {
		return
	}
	slices.SortFunc(sets, func(a, b set) int { return b.at.Compare(a.at) })
	for _, s := range sets[keptSkillSets:] {
		if s.path != keep {
			_ = os.RemoveAll(s.path)
		}
	}
}

// SkillDirs lists the skill folders of a written set, for a runtime that
// loads skills one folder at a time.
func SkillDirs(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && isSkillName(e.Name()) {
			out = append(out, filepath.Join(dir, "skills", e.Name()))
		}
	}
	return out
}

func isSkillHash(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// isSkillName is Agent Skills' rule: lowercase letters, digits and single
// hyphens, at most 64 characters.
func isSkillName(s string) bool {
	if s == "" || len(s) > 64 || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") || strings.Contains(s, "--") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// SkillAlias is the name a skill of the library goes by on a machine where
// a person's own skill has its name.
func SkillAlias(name string) string { return SkillPlugin + "-" + name }

// SkillAliases are the skills of the set that go by another name where
// taken are a person's own, by their name: the ones taken, unless the
// alias would be no skill name.
func SkillAliases(set *SkillSet, taken map[string]bool) map[string]string {
	aliases := map[string]string{}
	for _, s := range set.Skills {
		if alias := SkillAlias(s.Name); taken[s.Name] && isSkillName(alias) {
			aliases[s.Name] = alias
		}
	}
	return aliases
}

// aliasSuffix names the renames for the set's directory: hex, so the name
// stays one a set's directory has.
func aliasSuffix(aliases map[string]string) string {
	if len(aliases) == 0 {
		return ""
	}
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	slices.Sort(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return hex.EncodeToString(sum[:4])
}

// renameSkill gives a SKILL.md another name, on its frontmatter's name
// line, the first one.
func renameSkill(content, from, to string) string {
	re := regexp.MustCompile(`(?m)^name:[ \t]*` + regexp.QuoteMeta(from) + `[ \t]*$`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return content
	}
	return content[:loc[0]] + "name: " + to + content[loc[1]:]
}
