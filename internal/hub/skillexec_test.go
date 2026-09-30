package hub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
)

// tool stands in for a tool built for the machine: nothing in it says it
// runs, only its mode does.
var tool = []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0}

// A skill's tools run wherever it goes: what runs in its folder runs in
// the library's history, on the machine a turn takes it to and out of the
// zip it is taken away in; what does not, does not.
func TestHub_ImportSkill_Executables(t *testing.T) {
	l, dir := wikiLoop(t)
	folder := skillFolder(t, "tool-word", map[string]string{
		"SKILL.md":       "---\nname: tool-word\ndescription: Use when asked for the tool word.\n---\n\nRun bin/word.\n",
		"scripts/run.sh": "#!/bin/sh\nexec \"$(dirname \"$0\")/../bin/word\"\n",
	})
	for rel, mode := range map[string]fs.FileMode{"bin/word": 0o755, "bin/data": 0o644} {
		p := filepath.Join(folder, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, tool, mode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.h.ImportSkill(l.ctx, folder, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("git", "-C", filepath.Join(dir, "library"), "ls-files", "-s", "skills/tool-word").Output()
	var modes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		modes = append(modes, f[0]+" "+strings.TrimPrefix(f[3], "skills/tool-word/"))
	}
	if want := []string{"100644 SKILL.md", "100644 bin/data", "100755 bin/word", "100644 scripts/run.sh"}; !slices.Equal(modes, want) {
		t.Errorf("in the history %v, want %v", modes, want)
	}

	// To a turn: the tool runs, and so does the script by its #!.
	r, _ := l.h.openLibrary(l.ctx)
	skills := l.h.turns.skillsOf(r.bundle, "claude", func(name string) bool { return name == "tool-word" })
	if len(skills) != 1 || !slices.Equal(skills[0].Executable, []string{"bin/word", "scripts/run.sh"}) {
		t.Fatalf("to a turn: %+v", skills)
	}
	set, err := runtime.WriteSkills(t.TempDir(), &runtime.SkillSet{Hash: skillHash(skills), Skills: skills}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]fs.FileMode{"bin/word": 0o755, "bin/data": 0o644, "scripts/run.sh": 0o755} {
		if info, err := os.Stat(filepath.Join(set, "skills", "tool-word", filepath.FromSlash(rel))); err != nil || info.Mode().Perm() != want {
			t.Errorf("on the machine %s: %v %v, want %v", rel, info.Mode(), err, want)
		}
	}
	// Which files run is part of what names the set.
	still := []runtime.Skill{{Name: "tool-word", Files: skills[0].Files, Blobs: skills[0].Blobs, Executable: []string{"scripts/run.sh"}}}
	if skillHash(skills) == skillHash(still) {
		t.Error("the set's hash takes in which files run")
	}

	// Out of the zip.
	zipped, err := l.h.ExportSkill(l.ctx, "tool-word")
	if err != nil {
		t.Fatal(err)
	}
	archive, _ := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	got := map[string]fs.FileMode{}
	for _, f := range archive.File {
		got[f.Name] = f.Mode().Perm()
	}
	for rel, want := range map[string]fs.FileMode{"bin/word": 0o755, "bin/data": 0o644, "scripts/run.sh": 0o755, "SKILL.md": 0o644} {
		if got["tool-word/"+rel] != want {
			t.Errorf("in the zip %s: %v, want %v", rel, got["tool-word/"+rel], want)
		}
	}
}

// A zip's tools stay tools when it is uploaded.
func TestHub_UploadSkills_Executables(t *testing.T) {
	l, dir := wikiLoop(t)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, entry := range map[string]struct {
		data []byte
		mode fs.FileMode
	}{
		"tool-word/SKILL.md": {[]byte("---\nname: tool-word\ndescription: Use when asked for the tool word.\n---\n\nRun bin/word.\n"), 0o644},
		"tool-word/bin/word": {tool, 0o755},
		"tool-word/bin/data": {tool, 0o644},
	} {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(entry.data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.h.UploadSkills(l.ctx, SkillUpload{Name: "tool-word.zip", Zip: bytes.NewReader(buf.Bytes()), ZipSize: int64(buf.Len()), UserID: l.user.ID}); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]fs.FileMode{"bin/word": 0o755, "bin/data": 0o644} {
		if info, err := os.Stat(filepath.Join(dir, "library", "skills", "tool-word", filepath.FromSlash(rel))); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %v", rel, info.Mode(), err, want)
		}
	}
}

// A folder with nothing to run sums up as it did before the library kept
// which files run, so skills imported then show no change; one whose tool
// came in without the bit does, to be taken in again.
func TestSkillFolderHash(t *testing.T) {
	folder := skillFolder(t, "x", map[string]string{"SKILL.md": "a", "bin/tool": "bb"})
	files := []string{"bin/tool"}
	before := sha256.New()
	for _, f := range [][2]string{{"SKILL.md", "a"}, {"bin/tool", "bb"}} {
		fmt.Fprintf(before, "%s\x00%d\x00%s", f[0], len(f[1]), f[1])
	}
	got, err := skillFolderHash(folder, files)
	if err != nil || got != hex.EncodeToString(before.Sum(nil))[:16] {
		t.Errorf("nothing to run: %s %v", got, err)
	}
	os.Chmod(filepath.Join(folder, "bin", "tool"), 0o755)
	if runs, _ := skillFolderHash(folder, files); runs == got {
		t.Error("a tool made to run is a change")
	}
}
