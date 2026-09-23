package runtime

import (
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

func TestUserSkillNames(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for _, dir := range []string{
		filepath.Join(home, ".agents", "skills", "tdd"),
		filepath.Join(home, ".codex", "skills", "create-plan"),
		filepath.Join(work, ".pi", "skills", "local-one"),
	} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644)
	}
	// A folder without a SKILL.md is no skill.
	os.MkdirAll(filepath.Join(home, ".agents", "skills", "notes"), 0o755)
	got := UserSkillNames(work)
	if !got["tdd"] || !got["create-plan"] || !got["local-one"] || got["notes"] || len(got) != 3 {
		t.Errorf("taken %v", got)
	}
}
