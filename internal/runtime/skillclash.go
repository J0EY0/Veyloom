package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// A person's own skill and one Veyloom gives a turn by the same name
// (docs/design.md 5.11): the one installed for the agent is the one its
// turns use. Codex is told to turn the person's off (its skills.config,
// codexSkillsConfig). Claude Code cannot leave one skill out: it still
// lists the person's, so the run is told which to use and the person's is
// refused if called (claudeSkillClashes). Both for the turn only: the
// person's files and configuration stay as they are, and so does
// everything else they keep. Pi keeps a person's skill over another of its
// name and has no way to leave one out, so there the set's goes by another
// name (ClashingSkillNames).
//
// A skill a runtime comes with, or one an administrator put there, is no
// person's own: others keep it up to date. It stays on, under its name,
// and the set's of its name goes by another (ClashingSkillNames).

// ClashingSkillNames names the skills a skill of the set would clash with
// where runtimeName looks for skills, for the set to give its own another
// name there: on Pi a person's own, from their folders and the working
// directory's; on Codex the ones it comes with and an administrator's,
// which are not to be turned off.
func ClashingSkillNames(runtimeName, workDir string) map[string]bool {
	if runtimeName == "codex" {
		return namesOf(personsSkills(codexManagedSkillDirs()))
	}
	if runtimeName != "pi" {
		return nil
	}
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".pi", "agent", "skills"))
	}
	if workDir != "" {
		dirs = append(dirs, filepath.Join(workDir, ".agents", "skills"), filepath.Join(workDir, ".pi", "skills"))
	}
	return namesOf(personsSkills(dirs))
}

func namesOf(skills map[string][]string) map[string]bool {
	out := map[string]bool{}
	for name := range skills {
		out[name] = true
	}
	return out
}

// claudeSkillClashes names the skills of the set a person keeps a skill
// of the name of where Claude Code finds a person's: their home's
// .claude/skills, and the working directory's and those above it up to its
// repository's root. The set's come as the plugin's, veyloom:<name>.
func claudeSkillClashes(set *SkillSet, workDir string) []string {
	if set == nil || len(set.Skills) == 0 {
		return nil
	}
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".claude", "skills"))
	}
	for _, d := range upToRepoRoot(workDir) {
		dirs = append(dirs, filepath.Join(d, ".claude", "skills"))
	}
	persons := personsSkills(dirs)
	var out []string
	for _, s := range set.Skills {
		if _, ok := persons[s.Name]; ok {
			out = append(out, s.Name)
		}
	}
	return out
}

// claudeSkillNote tells a Claude Code run which of two skills of a name to
// use: the plugin's.
func claudeSkillNote(names []string) string {
	prefixed := make([]string, len(names))
	for i, name := range names {
		prefixed[i] = SkillPlugin + ":" + name
	}
	return "Skills installed for you in Veyloom come as " + strings.Join(prefixed, ", ") +
		". A skill of the same name without the " + SkillPlugin + ": prefix is a copy the person keeps for themselves and is not for this run: use the " +
		SkillPlugin + ": one."
}

// codexSkillsConfig is the skills.config a Codex turn runs with, as a TOML
// value for -c: the person's own entries, and one turning off each of the
// person's skills that a skill of the set has the name of, where Codex
// looks for a person's skills. "" when none has, or when the person's
// config.toml does not read: the -c would take the place of their own
// entries, so then the turn goes as they have it, and warning says why.
func codexSkillsConfig(set *SkillSet, workDir string) (config, warning string) {
	if set == nil || len(set.Skills) == 0 {
		return "", ""
	}
	names := map[string]bool{}
	for _, s := range set.Skills {
		names[s.Name] = true
	}
	var off []string
	for name, paths := range personsSkills(codexPersonsSkillDirs(workDir)) {
		if names[name] {
			off = append(off, paths...)
		}
	}
	if len(off) == 0 {
		return "", ""
	}
	slices.Sort(off)
	entries, ok := codexOwnSkillsConfig()
	if !ok {
		return "", "Codex's config.toml did not read, so in this turn your own skills of the names of the skill library's ones stay next to them."
	}
	for _, p := range slices.Compact(off) {
		entries = append(entries, "{path="+tomlString(p)+",enabled=false}")
	}
	return "[" + strings.Join(entries, ",") + "]", ""
}

// codexPersonsSkillDirs are the folders Codex takes a person's own skills
// from: their home's .agents/skills, Codex's own skills folder (not the
// system skills kept in it), and .agents/skills and .codex/skills from the
// working directory up to its repository's root.
func codexPersonsSkillDirs(workDir string) []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
	}
	if codexHome := codexHomeDir(); codexHome != "" {
		dirs = append(dirs, filepath.Join(codexHome, "skills"))
	}
	for _, d := range upToRepoRoot(workDir) {
		dirs = append(dirs, filepath.Join(d, ".agents", "skills"), filepath.Join(d, ".codex", "skills"))
	}
	return dirs
}

// codexManagedSkillDirs are the folders of the skills Codex comes with, in
// its skills folder's .system, and of an administrator's.
func codexManagedSkillDirs() []string {
	dirs := []string{"/etc/codex/skills"}
	if codexHome := codexHomeDir(); codexHome != "" {
		dirs = append(dirs, filepath.Join(codexHome, "skills", ".system"))
	}
	return dirs
}

// codexHomeDir is where Codex keeps its configuration and skills.
func codexHomeDir() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".codex")
	}
	return ""
}

// upToRepoRoot lists dir and the folders above it up to the one holding
// its repository's .git; dir alone when it is in no repository.
func upToRepoRoot(dir string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		out = append(out, d)
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return out
		}
		if filepath.Dir(d) == d {
			return []string{filepath.Clean(dir)}
		}
	}
}

// personsSkills finds the skills in dirs, each a folder holding a SKILL.md:
// by the name each gives itself, or its folder's, the paths of their
// SKILL.md, as found and, for a folder linked from elsewhere, where it is.
func personsSkills(dirs []string) map[string][]string {
	out := map[string][]string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, e.Name(), "SKILL.md")
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			name := frontmatterName(data)
			if name == "" {
				name = e.Name()
			}
			out[name] = append(out[name], p)
			if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
				out[name] = append(out[name], real)
			}
		}
	}
	return out
}

var nameLine = regexp.MustCompile(`(?m)^name:[ \t]*(.*?)[ \t]*$`)

// frontmatterName is the name a SKILL.md's frontmatter gives, "" for none.
func frontmatterName(data []byte) string {
	rest, ok := bytes.CutPrefix(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("---\n"))
	if !ok {
		return ""
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return ""
	}
	m := nameLine.FindSubmatch(front)
	if m == nil {
		return ""
	}
	return strings.Trim(string(m[1]), `"'`)
}

// codexOwnSkillsConfig is the person's own skills.config in Codex's
// config.toml, each entry as an inline table, for a turn's override to
// keep: a -c for skills.config replaces the list whole. ok is false when
// the file is there and does not read, or its skills.config is no list of
// tables; no file is none.
func codexOwnSkillsConfig() (entries []string, ok bool) {
	codexHome := codexHomeDir()
	if codexHome == "" {
		return nil, true
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		return nil, os.IsNotExist(err)
	}
	var config map[string]any
	if toml.Unmarshal(data, &config) != nil {
		return nil, false
	}
	skills, _ := config["skills"].(map[string]any)
	list, _ := skills["config"].([]any)
	if skills["config"] != nil && list == nil {
		return nil, false
	}
	var out []string
	for _, item := range list {
		entry, isTable := item.(map[string]any)
		if !isTable {
			return nil, false
		}
		var fields []string
		for _, key := range slices.Sorted(func(yield func(string) bool) {
			for k := range entry {
				if !yield(k) {
					return
				}
			}
		}) {
			if value, ok := tomlValue(entry[key]); ok {
				fields = append(fields, key+"="+value)
			}
		}
		if len(fields) > 0 {
			out = append(out, "{"+strings.Join(fields, ",")+"}")
		}
	}
	return out, true
}

// tomlValue writes a string, a boolean or a number as TOML.
func tomlValue(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return tomlString(v), true
	case bool:
		return strconv.FormatBool(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	}
	return "", false
}

// tomlString is s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			b.WriteString(`\u` + strings.ToUpper(strconv.FormatInt(int64(r)|0x10000, 16)[1:]))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
