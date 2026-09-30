package runtime

import (
	"bytes"
	"github.com/pelletier/go-toml/v2"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testSkillSet(hash string) *SkillSet {
	return &SkillSet{Hash: hash, Skills: []Skill{
		{Name: "go-table-tests", Files: map[string]string{"SKILL.md": "---\nname: go-table-tests\ndescription: d\n---\n\nUse tables.\n", "scripts/run.sh": "#!/bin/sh\ngo test ./...\n"}},
		{Name: "release-notes", Files: map[string]string{"SKILL.md": "---\nname: release-notes\ndescription: d\n---\n\nWrite them.\n"}},
	}}
}

func TestWriteSkills(t *testing.T) {
	root := t.TempDir()
	dir, err := WriteSkills(root, testSkillSet("0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "0123456789abcdef") {
		t.Errorf("dir %s", dir)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil || !strings.Contains(string(manifest), `"name": "veyloom"`) {
		t.Errorf("a Claude Code plugin: %s %v", manifest, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "skills", "go-table-tests", "scripts", "run.sh")); err != nil || info.Mode()&0o100 == 0 {
		t.Errorf("a script stays runnable: %v %v", info, err)
	}
	dirs := SkillDirs(dir)
	if len(dirs) != 2 || filepath.Base(dirs[0]) != "go-table-tests" {
		t.Errorf("skill folders %v", dirs)
	}
	// A file that is not text lands as it came, byte for byte.
	blob := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe, '\n'}
	withBlob := testSkillSet("0123456789abcdee")
	withBlob.Skills[0].Blobs = map[string][]byte{"assets/logo.png": blob}
	blobDir, err := WriteSkills(t.TempDir(), withBlob, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(blobDir, "skills", "go-table-tests", "assets", "logo.png")); err != nil || !bytes.Equal(got, blob) {
		t.Errorf("the picture %v %v", got, err)
	}
	// A tool built for the machine runs when the skill says it does; the
	// picture, not.
	withTool := testSkillSet("0123456789abcded")
	withTool.Skills[0].Blobs = map[string][]byte{"bin/tool": {0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, "assets/logo.png": blob}
	withTool.Skills[0].Executable = []string{"bin/tool"}
	toolDir, err := WriteSkills(t.TempDir(), withTool, nil)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]fs.FileMode{"bin/tool": 0o755, "assets/logo.png": 0o644, "scripts/run.sh": 0o755} {
		if info, err := os.Stat(filepath.Join(toolDir, "skills", "go-table-tests", filepath.FromSlash(rel))); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %v", rel, info.Mode(), err, want)
		}
	}
	// The same set again is not written again.
	os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644)
	if again, err := WriteSkills(root, testSkillSet("0123456789abcdef"), nil); err != nil || again != dir {
		t.Fatal(again, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Error("an existing set is kept as it is")
	}
	if dir, err := WriteSkills(root, nil, nil); dir != "" || err != nil {
		t.Errorf("no set, no folder: %q %v", dir, err)
	}
	for name, set := range map[string]*SkillSet{
		"a hash that is none":   {Hash: "../x", Skills: testSkillSet("").Skills},
		"a name that is none":   {Hash: "0123456789abcde0", Skills: []Skill{{Name: "Bad Name", Files: map[string]string{"SKILL.md": "x"}}}},
		"a file outside":        {Hash: "0123456789abcde1", Skills: []Skill{{Name: "x", Files: map[string]string{"SKILL.md": "x", "../escape": "x"}}}},
		"a skill with no SKILL": {Hash: "0123456789abcde2", Skills: []Skill{{Name: "x", Files: map[string]string{"README.md": "x"}}}},
		"a blob outside":        {Hash: "0123456789abcde3", Skills: []Skill{{Name: "x", Files: map[string]string{"SKILL.md": "x"}, Blobs: map[string][]byte{"../escape": {0}}}}},
	} {
		if _, err := WriteSkills(root, set, nil); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Errorf("a refused set leaves nothing behind: %d entries", len(entries))
	}
}

func TestWriteSkills_KeepsTheLatestSets(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-time.Hour)
	for i := range keptSkillSets + 3 {
		hash := "00000000000000" + strconv.FormatInt(int64(10+i), 16)
		dir, err := WriteSkills(root, testSkillSet(hash), nil)
		if err != nil {
			t.Fatal(err)
		}
		at := old.Add(time.Duration(i) * time.Minute)
		os.Chtimes(dir, at, at)
	}
	latest, _ := WriteSkills(root, testSkillSet("ffffffffffffffff"), nil)
	entries, _ := os.ReadDir(root)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != keptSkillSets || !slices.Contains(names, filepath.Base(latest)) || slices.Contains(names, "000000000000000a") {
		t.Errorf("kept %d sets: %v", len(names), names)
	}
}

// A skill of the library whose name a person's own skill has goes by its
// alias, in a set directory of its own; Pi would otherwise keep only the
// person's.
func TestWriteSkills_RenamesWhatAPersonsSkillsHave(t *testing.T) {
	root := t.TempDir()
	plain, err := WriteSkills(root, testSkillSet("0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := WriteSkills(root, testSkillSet("0123456789abcdef"), map[string]bool{"release-notes": true, "unrelated": true})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(dir)
	if dir == plain || !strings.HasPrefix(base, "0123456789abcdef") || !isSkillHash(base) {
		t.Fatalf("the renamed set's directory %s", dir)
	}
	skill, err := os.ReadFile(filepath.Join(dir, "skills", "veyloom-release-notes", "SKILL.md"))
	if err != nil || !strings.Contains(string(skill), "name: veyloom-release-notes\n") || strings.Contains(string(skill), "name: release-notes") {
		t.Errorf("the renamed skill %q %v", skill, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "go-table-tests", "SKILL.md")); err != nil {
		t.Errorf("the other keeps its name: %v", err)
	}
	if got := SkillDirs(dir); len(got) != 2 {
		t.Errorf("dirs %v", got)
	}
	if again, _ := WriteSkills(root, testSkillSet("0123456789abcdef"), map[string]bool{"release-notes": true}); again != dir {
		t.Errorf("the same renames reuse the directory: %s, want %s", again, dir)
	}
}

// Only Pi's clashes rename a skill of the set: Claude Code and Codex leave
// the person's same-named skill out of the turn instead. A skill is known
// by the name it gives itself, else by its folder's.
func TestClashingSkillNames(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for dir, front := range map[string]string{
		filepath.Join(home, ".agents", "skills", "tdd"):            "---\nname: tdd\n---\n",
		filepath.Join(home, ".pi", "agent", "skills", "folder"):    "---\nname: \"named-inside\"\n---\n",
		filepath.Join(home, ".codex", "skills", "create-plan"):     "---\nname: create-plan\n---\n",
		filepath.Join(work, ".pi", "skills", "local-one"):          "---\ndescription: d\n---\n",
		filepath.Join(work, ".agents", "skills", "no-frontmatter"): "just text",
	} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(front), 0o644)
	}
	// A folder without a SKILL.md is no skill.
	os.MkdirAll(filepath.Join(home, ".agents", "skills", "notes"), 0o755)
	got := ClashingSkillNames("pi", work)
	want := []string{"local-one", "named-inside", "no-frontmatter", "tdd"}
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, want) {
		t.Errorf("pi's %v, want %v", names, want)
	}
	for _, runtime := range []string{"claude", "codex", "fake"} {
		if got := ClashingSkillNames(runtime, work); len(got) != 0 {
			t.Errorf("%s renames %v", runtime, got)
		}
	}
}

// Codex runs a turn with the person's own skills of the set's names off,
// found where Codex looks, up to the repository's root, by what they call
// themselves; the person's own entries are kept.
func TestCodexSkillsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHome := filepath.Join(home, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	repo := t.TempDir()
	work := filepath.Join(repo, "services", "api")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	elsewhere := t.TempDir()
	for dir, name := range map[string]string{
		filepath.Join(home, ".agents", "skills", "pdf"):            "pdf",
		filepath.Join(codexHome, "skills", ".system", "imagegen"):  "imagegen",
		filepath.Join(repo, ".agents", "skills", "release-notes"):  "release-notes",
		filepath.Join(work, ".codex", "skills", "renamed-folder"):  "go-table-tests",
		filepath.Join(elsewhere, "linked"):                         "linked",
		filepath.Join(home, ".agents", "skills", "not-in-the-set"): "not-in-the-set",
	} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644)
	}
	os.Symlink(filepath.Join(elsewhere, "linked"), filepath.Join(home, ".agents", "skills", "linked"))
	os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n\n[[skills.config]]\npath = \"/opt/old/SKILL.md\"\nenabled = false\n"), 0o644)

	set := &SkillSet{Skills: []Skill{{Name: "pdf"}, {Name: "imagegen"}, {Name: "release-notes"}, {Name: "go-table-tests"}, {Name: "linked"}, {Name: "fresh"}}}
	got, warning := codexSkillsConfig(set, work)
	if warning != "" {
		t.Errorf("warning %q", warning)
	}
	var parsed struct {
		V []struct {
			Path    string `toml:"path"`
			Enabled bool   `toml:"enabled"`
		} `toml:"v"`
	}
	if err := toml.Unmarshal([]byte("v = "+got), &parsed); err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	// Each as found and, where a link leads elsewhere, where it is.
	var off []string
	for _, e := range parsed.V {
		if e.Enabled {
			t.Errorf("%s is on", e.Path)
		}
		off = append(off, e.Path)
	}
	want := []string{"/opt/old/SKILL.md"}
	for _, p := range []string{
		filepath.Join(home, ".agents", "skills", "pdf", "SKILL.md"),
		filepath.Join(repo, ".agents", "skills", "release-notes", "SKILL.md"),
		filepath.Join(work, ".codex", "skills", "renamed-folder", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "linked", "SKILL.md"),
	} {
		want = append(want, p)
		if real, _ := filepath.EvalSymlinks(p); real != p {
			want = append(want, real)
		}
	}
	slices.Sort(want[1:])
	if !slices.Equal(off, want) {
		t.Errorf("off:\n%s\nwant:\n%s", strings.Join(off, "\n"), strings.Join(want, "\n"))
	}
	// A skill Codex comes with stays on: OpenAI keeps it up to date. The
	// set's of its name goes by another name instead (ClashingSkillNames).
	if strings.Contains(got, "imagegen") {
		t.Errorf("Codex's own imagegen is turned off: %s", got)
	}
	if clashes := ClashingSkillNames("codex", work); !clashes["imagegen"] || len(clashes) != 1 {
		t.Errorf("codex renames beside %v, want its own imagegen only", clashes)
	}
	if got, _ := codexSkillsConfig(&SkillSet{Skills: []Skill{{Name: "fresh"}}}, work); got != "" {
		t.Errorf("nothing of the person's to turn off: %q", got)
	}
	// A config.toml that does not read: the turn goes as the person has it,
	// the override taking the place of their own entries, and says why.
	os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[[skills.config]\npath = \n"), 0o644)
	if got, warning := codexSkillsConfig(set, work); got != "" || warning == "" {
		t.Errorf("an unreadable config.toml: %q %q", got, warning)
	}
	// Nor is one whose skills.config is no list of tables used.
	os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[skills]\nconfig = \"off\"\n"), 0o644)
	if got, warning := codexSkillsConfig(set, work); got != "" || warning == "" {
		t.Errorf("an odd skills.config: %q %q", got, warning)
	}
	// No config.toml at all is none to keep.
	os.Remove(filepath.Join(codexHome, "config.toml"))
	if got, warning := codexSkillsConfig(set, work); got == "" || warning != "" || strings.Contains(got, "/opt/old") {
		t.Errorf("no config.toml: %q %q", got, warning)
	}
	if got := tomlString("a\"b\\c\n"); got != `"a\"b\\c\u000A"` {
		t.Errorf("tomlString %s", got)
	}
}
